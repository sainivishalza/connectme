import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:libsignal_protocol_dart/libsignal_protocol_dart.dart' show UntrustedIdentityException;

import '../../core/network/api_client.dart';
import 'chat_providers.dart';
import 'data/chat_models.dart';
import 'data/chat_repository.dart';
import 'data/chat_security.dart';
import 'data/message_cache.dart';
import 'data/realtime_client.dart';
import 'voice/voice_io.dart';

class ChatState {
  const ChatState({
    this.messages = const [],
    this.peerTyping = false,
    this.burnAfterS = 0,
    this.identityChanged = false,
  });
  final List<ChatMessage> messages;
  final bool peerTyping;
  final int burnAfterS;

  /// The other person's security key is different from the one we trusted.
  /// Nothing is sent or shown from them until it has been reviewed.
  final bool identityChanged;

  ChatState copyWith({
    List<ChatMessage>? messages,
    bool? peerTyping,
    int? burnAfterS,
    bool? identityChanged,
  }) => ChatState(
    messages: messages ?? this.messages,
    peerTyping: peerTyping ?? this.peerTyping,
    burnAfterS: burnAfterS ?? this.burnAfterS,
    identityChanged: identityChanged ?? this.identityChanged,
  );

  /// What the chat shows: messages whose burn timer (counted from when they
  /// were read) has not run out, minus the ones that were unsent, with each
  /// message's reactions attached. Reactions and unsend notices are control
  /// messages in the same encrypted stream; they are applied here, in order,
  /// and never shown as bubbles. A notice only counts when it comes from the
  /// message's own sender, so nobody can make the other person's words vanish.
  List<ChatMessage> visible(DateTime now) {
    final byId = {for (final m in messages) m.id: m};
    final gone = <String>{};
    final reacts = <String, Map<String, String>>{};
    for (final c in messages) {
      if (c.kind != MsgKind.control || c.ctlTarget == null) continue;
      final target = byId[c.ctlTarget];
      if (c.ctl == 'del') {
        if (target != null && target.fromMe == c.fromMe) gone.add(target.id);
      } else if (c.ctl == 'react' && target != null) {
        final who = reacts.putIfAbsent(target.id, () => {});
        c.emoji.isEmpty ? who.remove(c.fromMe ? 'me' : 'them') : who[c.fromMe ? 'me' : 'them'] = c.emoji;
      }
    }
    return [
      for (final m in messages)
        if (m.kind != MsgKind.control &&
            !gone.contains(m.id) &&
            !(m.burnAfterS > 0 && m.readAt != null && now.isAfter(m.readAt!.add(Duration(seconds: m.burnAfterS)))))
          reacts.containsKey(m.id) ? m.copyWith(reactions: reacts[m.id]) : m,
    ];
  }
}

/// Family key: conversation id + whether it is a merged group chat.
typedef ConvKey = ({String id, bool group});

final chatControllerProvider = AsyncNotifierProvider.autoDispose
    .family<ChatController, ChatState, ConvKey>(ChatController.new);

class ChatController extends AsyncNotifier<ChatState> {
  ChatController(this.key);
  final ConvKey key;

  String get _id => key.id;
  ChatSecurity get _sec => ref.read(chatSecurityProvider)!;
  MessageCache get _cache => ref.read(messageCacheProvider)!;
  ChatRepository get _repo => ref.read(chatRepositoryProvider);
  RealtimeClient? get _rt => ref.read(realtimeProvider);

  final _subs = <StreamSubscription<dynamic>>[];
  Timer? _typingOff;
  bool _identityChanged = false;

  @override
  Future<ChatState> build() async {
    // Captured now: Ref must not be used inside dispose callbacks.
    final rt = ref.read(realtimeProvider);
    ref.onDispose(() {
      for (final s in _subs) {
        s.cancel();
      }
      _typingOff?.cancel();
      rt?.leave(_id);
    });
    await _sec.ensureReady();
    if (key.group) {
      try {
        await _sec.prepareGroup(_id);
      } catch (_) {
        // Keys may not all be published yet; retried on refresh.
      }
    }
    final msgs = await _loadAll();

    if (rt != null) {
      unawaited(rt.join(_id));
      _subs.addAll([
        rt.messages.listen(_onIncoming),
        rt.typing.listen((e) {
          if (e['match_id'] == _id) _setTyping(e['is_typing'] == true);
        }),
        rt.receipts.listen((e) {
          if (e['match_id'] == _id) _markMineRead(DateTime.now());
        }),
      ]);
    }
    unawaited(_markRead());
    return ChatState(messages: msgs, identityChanged: _identityChanged);
  }

  // --- loading ---------------------------------------------------------

  Future<List<ChatMessage>> _loadAll() async {
    final wire = await _repo.messages(_id);
    final cached = await _cache.load(_id);
    final out = <ChatMessage>[];
    final keep = <String, ChatMessage>{};
    for (final w in wire) {
      var m = cached[w.id];
      m ??= await _decrypt(w);
      if (m.kind != MsgKind.locked) {
        keep[w.id] = m; // only successes are cached (locked ones may be retried)
      }
      out.add(w.readAt != null ? m.copyWith(readAt: w.readAt) : m);
    }
    // Drop cached messages that are gone from the server (burned/deleted),
    // but only inside the window this page covers.
    final oldest = wire.isEmpty ? null : wire.first.id;
    for (final e in cached.entries) {
      if (!keep.containsKey(e.key) &&
          oldest != null &&
          e.key.compareTo(oldest) < 0) {
        keep[e.key] = e.value;
      }
    }
    await _cache.save(_id, keep);
    return out;
  }

  Future<ChatMessage> _decrypt(WireMessage w) async {
    ChatMessage locked(String why) => ChatMessage(
      id: w.id,
      fromMe: w.fromMe,
      kind: MsgKind.locked,
      text: why,
      createdAt: w.createdAt,
      burnAfterS: w.burnAfterS,
    );
    if (w.fromMe) return locked('Sent from another device');
    try {
      final Uint8List bytes = key.group
          ? await _sec.decryptGroup(_id, w.senderToken, w.ciphertext)
          : await _sec.decryptDirect(_id, w.ciphertext);
      final p = MessagePayload.decode(bytes);
      if (p == null) return locked('Unsupported message');
      if (p.ctl != null) {
        return ChatMessage(
          id: w.id,
          fromMe: false,
          kind: MsgKind.control,
          ctl: p.ctl,
          ctlTarget: p.target,
          emoji: p.emoji,
          burnAfterS: 0,
          createdAt: w.createdAt,
          readAt: w.readAt,
        );
      }
      return ChatMessage(
        replyToId: p.replyTo,
        replyQuote: p.quote,
        id: w.id,
        fromMe: false,
        kind: p.type == 'image'
            ? MsgKind.image
            : (p.type == 'voice' ? MsgKind.voice : MsgKind.text),
        text: p.text,
        mediaId: w.mediaId,
        mediaKey: p.key,
        mime: p.mime,
        burnAfterS: w.burnAfterS,
        createdAt: w.createdAt,
        readAt: w.readAt,
      );
    } on UntrustedIdentityException {
      _identityChanged = true;
      return locked('Their security key changed. Review it under Verify security.');
    } catch (_) {
      return locked("Couldn't decrypt this message");
    }
  }

  /// Reload from the server (pull-to-refresh), re-fetching group keys first.
  Future<void> refresh() async {
    if (key.group) {
      try {
        await _sec.prepareGroup(_id);
      } catch (_) {}
    }
    final msgs = await _loadAll();
    state = AsyncData(
      (state.value ?? const ChatState()).copyWith(messages: msgs, identityChanged: _identityChanged),
    );
  }

  // --- realtime --------------------------------------------------------

  Future<void> _onIncoming(Map<String, dynamic> e) async {
    if (e['match_id'] != _id) return;
    final w = WireMessage.fromJson(e);
    final cur = state.value;
    if (cur == null || cur.messages.any((m) => m.id == w.id)) return;
    final m = await _decrypt(w);
    if (m.kind != MsgKind.locked) {
      final c = await _cache.load(_id);
      c[w.id] = m;
      await _cache.save(_id, c);
    }
    state = AsyncData(
      cur.copyWith(
        messages: [...(state.value ?? cur).messages, m],
        peerTyping: false,
        identityChanged: _identityChanged,
      ),
    );
    if (!w.fromMe) unawaited(_markRead());
  }

  void _setTyping(bool v) {
    final cur = state.value;
    if (cur == null) return;
    _typingOff?.cancel();
    state = AsyncData(cur.copyWith(peerTyping: v));
    if (v) {
      _typingOff = Timer(const Duration(seconds: 6), () => _setTyping(false));
    }
  }

  void _markMineRead(DateTime at) {
    final cur = state.value;
    if (cur == null) return;
    state = AsyncData(
      cur.copyWith(
        messages: [
          for (final m in cur.messages)
            m.fromMe && m.readAt == null ? m.copyWith(readAt: at) : m,
        ],
      ),
    );
  }

  Future<void> _markRead() async {
    try {
      final rt = _rt;
      if (rt != null && rt.connected && await rt.readReceipt(_id)) return;
      await _repo.markRead(_id);
    } catch (_) {
      // Non-critical.
    }
  }

  void typing(bool v) => _rt?.setTyping(_id, v);

  void setBurn(int seconds) {
    final cur = state.value;
    if (cur != null) state = AsyncData(cur.copyWith(burnAfterS: seconds));
  }

  // --- sending ---------------------------------------------------------

  /// Refuses to send while the other person's security key is unreviewed:
  /// anything sent now would go to a key we no longer trust.
  void _requireTrustedKey() {
    if (_identityChanged) {
      throw ApiException(409, 'Review their new security key first (menu > Verify security).');
    }
  }

  /// The safety number for this chat (one-to-one only), or null if there is
  /// no session yet.
  Future<String?> safetyNumber() async => key.group ? null : _sec.safetyNumber(_id);

  /// Accepts the other person's new key after review, then reloads.
  Future<void> trustNewKey() async {
    await _sec.trustNewIdentity(_id);
    _identityChanged = false;
    final cur = state.value ?? const ChatState();
    state = AsyncData(cur.copyWith(identityChanged: false));
    await refresh();
  }

  /// [replyTo] makes this a reply: it carries the other message's id and a
  /// short quote of it, inside the encrypted payload.
  Future<void> sendText(String text, {ChatMessage? replyTo}) async {
    final t = text.trim();
    if (t.isEmpty) return;
    _requireTrustedKey();
    final quote = replyTo == null || replyTo.id.startsWith('tmp-') ? null : excerpt(replyTo);
    await _send(
      MessagePayload.text(t.length > 4000 ? t.substring(0, 4000) : t, replyTo: quote == null ? null : replyTo!.id, quote: quote ?? ''),
      'text',
      null,
    );
  }

  /// A short, plain description of a message for the quote in a reply.
  static String excerpt(ChatMessage m) {
    switch (m.kind) {
      case MsgKind.image:
        return 'Photo';
      case MsgKind.voice:
        return 'Voice note';
      case MsgKind.text:
        final t = m.text.replaceAll('\n', ' ').trim();
        return t.length > 80 ? '${t.substring(0, 80)}…' : t;
      default:
        return '';
    }
  }

  /// Reacts to [m] with [emoji]; reacting with the same emoji again takes it back.
  Future<void> react(ChatMessage m, String emoji) async {
    if (m.id.startsWith('tmp-') || m.kind == MsgKind.control) return;
    _requireTrustedKey();
    final mine = state.value?.visible(DateTime.now()).where((x) => x.id == m.id).firstOrNull?.reactions['me'];
    await _send(MessagePayload.react(m.id, mine == emoji ? '' : emoji), 'text', null);
  }

  /// Unsends one of my own messages for everyone: it is deleted on the server,
  /// then the other person's app is told so it disappears there right away.
  Future<void> unsend(ChatMessage m) async {
    if (!m.fromMe || m.pending || m.id.startsWith('tmp-') || m.kind == MsgKind.control) return;
    await _repo.unsendMessage(_id, m.id);
    final cur = state.value;
    if (cur != null) state = AsyncData(cur.copyWith(messages: cur.messages.where((x) => x.id != m.id).toList()));
    final c = await _cache.load(_id);
    c.remove(m.id);
    await _cache.save(_id, c);
    try {
      await _send(MessagePayload.unsend(m.id), 'text', null);
    } catch (_) {
      // The notice is only a courtesy; the server copy is already gone.
    }
  }

  Future<void> sendImage(Uint8List bytes, {String mime = 'image/jpeg'}) async {
    _requireTrustedKey();
    final enc = await ChatSecurity.encryptAttachment(bytes);
    final mediaId = await _repo.uploadMedia(_id, enc.blob);
    await _send(
      MessagePayload.media(type: 'image', key: enc.key, mime: mime),
      'image',
      mediaId,
    );
  }

  /// Encrypts a recorded clip like any attachment and sends it as a voice
  /// note; the duration travels inside the encrypted payload.
  Future<void> sendVoice(VoiceClip clip) async {
    _requireTrustedKey();
    final enc = await ChatSecurity.encryptAttachment(clip.bytes);
    final mediaId = await _repo.uploadMedia(_id, enc.blob);
    await _send(
      MessagePayload.media(
        type: 'voice',
        key: enc.key,
        mime: clip.mime,
        text: '${clip.seconds}',
      ),
      'voice',
      mediaId,
    );
  }

  Future<void> _send(MessagePayload p, String type, String? mediaId) async {
    final cur = state.value ?? const ChatState();
    final burn = cur.burnAfterS;
    final tmpId = 'tmp-${DateTime.now().microsecondsSinceEpoch}';
    final pending = ChatMessage(
      id: tmpId,
      fromMe: true,
      kind: p.ctl != null
          ? MsgKind.control
          : (type == 'image' ? MsgKind.image : (type == 'voice' ? MsgKind.voice : MsgKind.text)),
      text: p.text,
      mediaId: mediaId,
      mediaKey: p.key,
      mime: p.mime,
      burnAfterS: p.ctl != null ? 0 : burn,
      createdAt: DateTime.now(),
      pending: true,
      replyToId: p.replyTo,
      replyQuote: p.quote,
      ctl: p.ctl,
      ctlTarget: p.target,
      emoji: p.emoji,
    );
    state = AsyncData(cur.copyWith(messages: [...cur.messages, pending]));

    try {
      final ct = key.group
          ? await _sec.encryptGroup(_id, p.encode())
          : await _sec.encryptDirect(_id, p.encode());
      final body = {
        'type': type,
        'ciphertext': ct,
        'media_id': ?mediaId,
        'burn_after_s': burn,
      };
      final viaSocket = await _rt?.sendMessage(_id, body);
      final stored = viaSocket != null
          ? WireMessage.fromJson(viaSocket)
          : await _repo.send(
              _id,
              type: type,
              ciphertextB64: ct,
              mediaId: mediaId,
              burnAfterS: burn,
            );

      final sent = pending.copyWith(id: stored.id, pending: false);
      final c = await _cache.load(_id);
      c[stored.id] = sent;
      await _cache.save(_id, c);
      final now = state.value ?? cur;
      // A socket echo may already have added it; keep exactly one copy.
      final without = now.messages
          .where((m) => m.id != tmpId && m.id != stored.id)
          .toList();
      state = AsyncData(now.copyWith(messages: [...without, sent]));
    } catch (e) {
      final now = state.value ?? cur;
      state = AsyncData(
        now.copyWith(
          messages: now.messages.where((m) => m.id != tmpId).toList(),
        ),
      );
      rethrow;
    }
  }

  /// Decrypts an attachment for display.
  Future<Uint8List> openAttachment(ChatMessage m) async {
    if (m.mediaId == null || m.mediaKey == null) {
      throw ApiException(0, 'Attachment unavailable');
    }
    final blob = await _repo.downloadMedia(m.mediaId!);
    return ChatSecurity.decryptAttachment(blob, m.mediaKey!);
  }

  Future<void> deleteConversation() async {
    await _repo.deleteConversation(_id);
    await _cache.clear(_id);
  }
}
