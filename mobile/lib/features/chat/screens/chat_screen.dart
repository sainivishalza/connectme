import 'dart:async';

import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';

import '../../random_chat/widgets/random_banner.dart';
import '../widgets/reveal_timer_banner.dart';
import '../widgets/risk_banner.dart';
import '../widgets/voice_step_banner.dart';
import '../data/safety_scan.dart';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:image_picker/image_picker.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../discovery/discovery_repository.dart';
import '../../group_dates/groups_repository.dart';
import '../../payments/screens/payments_screens.dart';
import '../chat_controller.dart';
import '../data/chat_models.dart';
import '../data/chat_repository.dart';
import '../voice/voice_composer.dart';
import '../../games/widgets/games_sheet.dart';
import '../../calls/calls_repository.dart';
import '../widgets/chat_sheets.dart';
import '../widgets/wingman_reply_sheet.dart';
import '../data/scam_check.dart';
import '../widgets/gif_picker.dart';
import '../widgets/message_bubble.dart';
import '../widgets/message_menu.dart';

/// The round send / voice button next to the message box: the app's coral to
/// pink with a dark icon, so it reads as the one thing to press.
final ButtonStyle _composerAction = IconButton.styleFrom(
  backgroundColor: ModeLight.daylight.start,
  foregroundColor: ModeLight.daylight.ink,
  fixedSize: const Size(48, 48),
);

class ChatScreen extends ConsumerStatefulWidget {
  const ChatScreen({
    super.key,
    required this.convId,
    required this.isGroup,
    this.openGames = false,
    this.focusId,
  });

  /// A message to scroll to and highlight when the chat opens (from search).
  final String? focusId;
  final String convId;
  final bool isGroup;

  /// Masked Truth opens straight into the game sheet.
  final bool openGames;

  @override
  ConsumerState<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends ConsumerState<ChatScreen> {
  final _text = TextEditingController();
  bool _recording = false;
  bool _hasText = false;
  ChatMessage? _replyTo; // the message the next text replies to
  final _scroll = ScrollController();
  final _focusKey = GlobalKey();
  String? _pendingFocus; // jump here once the messages are on screen
  String? _dismissedRisk; // id of the message whose warning was dismissed
  String? _highlight;
  Timer? _highlightOff;
  Timer? _tick;
  Timer? _typingDebounce;
  bool _typingSent = false;

  ConvKey get _key => (id: widget.convId, group: widget.isGroup);

  @override
  void initState() {
    super.initState();
    _pendingFocus = widget.focusId;
    // Programmatic changes (icebreaker ideas, clearing after send) must update the
    // send/mic button too, not only typing.
    _text.addListener(() {
      final has = _text.text.trim().isNotEmpty;
      if (has != _hasText && mounted) setState(() => _hasText = has);
    });
    if (widget.openGames) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && !widget.isGroup) context.push('/td/${widget.convId}');
      });
    }
    // Re-evaluates burn timers, and only when some message can actually burn:
    // rebuilding the whole conversation every 2 seconds for nothing wastes
    // battery on long chats.
    _tick = Timer.periodic(const Duration(seconds: 2), (_) {
      if (!mounted) return;
      final msgs = ref.read(chatControllerProvider(_key)).value?.messages ?? const <ChatMessage>[];
      if (msgs.any((m) => m.burnAfterS > 0)) setState(() {});
    });
  }

  @override
  void dispose() {
    _highlightOff?.cancel();
    _scroll.dispose();
    _tick?.cancel();
    _typingDebounce?.cancel();
    _text.dispose();
    super.dispose();
  }

  Conversation? _conv() => ref
      .watch(conversationsProvider)
      .maybeWhen(
        data: (l) => l.where((c) => c.id == widget.convId).firstOrNull,
        orElse: () => null,
      );

  void _onChanged(String v) {
    final c = ref.read(chatControllerProvider(_key).notifier);
    if (!_typingSent && v.isNotEmpty) {
      _typingSent = true;
      c.typing(true);
    }
    _typingDebounce?.cancel();
    _typingDebounce = Timer(const Duration(seconds: 2), () {
      _typingSent = false;
      c.typing(false);
    });
  }

  Future<void> _send() async {
    final t = _text.text;
    if (t.trim().isEmpty) return;
    final replyTo = _replyTo;
    _text.clear();
    setState(() => _replyTo = null);
    await guarded(
      context,
      () => ref.read(chatControllerProvider(_key).notifier).sendText(t, replyTo: replyTo),
    );
  }

  /// Scrolls to a message and highlights it for a moment. The list is built
  /// lazily and bubbles differ in height, so this feels its way: it jumps to an
  /// estimate and then works outwards until the message is on screen.
  Future<void> _jumpTo(String id, List<ChatMessage> msgs) async {
    final idx = msgs.indexWhere((m) => m.id == id);
    if (idx < 0) {
      if (mounted) showMessage(context, 'That message is no longer available');
      return;
    }
    setState(() => _highlight = id);
    _highlightOff?.cancel();
    _highlightOff = Timer(const Duration(seconds: 3), () {
      if (mounted) setState(() => _highlight = null);
    });
    final fromEnd = msgs.length - 1 - idx;
    final guess = fromEnd * 72.0;
    for (var i = 0; i < 40 && mounted && _scroll.hasClients; i++) {
      final step = ((i + 1) ~/ 2) * 260.0 * (i.isOdd ? 1 : -1);
      _scroll.jumpTo((guess + step).clamp(0.0, _scroll.position.maxScrollExtent));
      await WidgetsBinding.instance.endOfFrame;
      final ctx = _focusKey.currentContext;
      if (ctx != null && ctx.mounted) {
        await Scrollable.ensureVisible(ctx, alignment: 0.5, duration: const Duration(milliseconds: 250));
        return;
      }
    }
  }

  Future<void> _messageMenu(ChatMessage m) async {
    if (m.kind == MsgKind.locked || m.pending) return;
    final ctrl = ref.read(chatControllerProvider(_key).notifier);
    final action = await showMessageMenu(context, m);
    if (action == null || !mounted) return;
    switch (action) {
      case ReactAction(:final emoji):
        await guarded(context, () => ctrl.react(m, emoji));
      case ReplyAction():
        setState(() => _replyTo = m);
      case UnsendAction():
        await guarded(context, () => ctrl.unsend(m));
    }
  }

  Future<void> _call({required bool video}) async {
    await guarded(context, () async {
      final started = await ref
          .read(callsRepositoryProvider)
          .start(widget.convId, video: video);
      if (mounted) context.push('/call', extra: started);
    });
  }

  Future<void> _attach() async {
    final x = await ImagePicker().pickImage(
      source: ImageSource.gallery,
      maxWidth: 1600,
      imageQuality: 85,
    );
    if (x == null || !mounted) return;
    final bytes = await x.readAsBytes();
    if (!mounted) return;
    await guarded(
      context,
      () => ref
          .read(chatControllerProvider(_key).notifier)
          .sendImage(bytes, mime: x.mimeType ?? 'image/jpeg'),
    );
  }

  Future<void> _gif() async {
    final bytes = await showGifPicker(context);
    if (bytes == null || !mounted) return;
    await guarded(
      context,
      () => ref.read(chatControllerProvider(_key).notifier).sendImage(bytes, mime: 'image/gif'),
    );
  }

  /// Shows the safety number for this chat, and, when the other person's key
  /// changed, lets the person accept the new key after reviewing it.
  Future<void> _verify() async {
    final ctrl = ref.read(chatControllerProvider(_key).notifier);
    final number = await ctrl.safetyNumber();
    if (!mounted) return;
    final changed = ref.read(chatControllerProvider(_key)).value?.identityChanged == true;
    final trust = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: Text(AppLocalizations.of(context)?.sVerifySecurity ?? 'Verify security'),
        content: SingleChildScrollView(
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
            if (changed) ...[
              Text('Their security key changed.', style: Theme.of(c).textTheme.titleSmall?.copyWith(color: Theme.of(c).colorScheme.error)),
              const SizedBox(height: 6),
              const Text('This is normal if they reinstalled the app. If you did not expect it, ask them to confirm the number below before you trust it.'),
              const SizedBox(height: 12),
            ],
            if (number == null)
              const Text('There is no secure session with this person yet. Send or receive a message first.')
            else ...[
              const Text('If you and your match see the same number, nobody is listening in between. Compare it in person or over another channel.'),
              const SizedBox(height: 12),
              SelectableText(number, key: const Key('safety_number'), style: const TextStyle(fontFeatures: [FontFeature.tabularFigures()], fontSize: 18, height: 1.6)),
            ],
          ]),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: Text(AppLocalizations.of(context)?.sClose ?? 'Close')),
          if (changed) FilledButton(key: const Key('trust_new_key'), onPressed: () => Navigator.pop(c, true), child: Text(AppLocalizations.of(context)?.sTrustTheNewKey ?? 'Trust the new key')),
        ],
      ),
    );
    if (trust == true && mounted) await guarded(context, ctrl.trustNewKey);
  }

  Future<void> _menu(String action, Conversation? conv) async {
    final repo = ref.read(discoveryRepositoryProvider);
    switch (action) {
      case 'reveal':
        final mutual = await _try(() => repo.reveal(widget.convId));
        if (mutual == null || !mounted) return;
        showMessage(
          context,
          mutual
              ? 'You both revealed. Photos are now visible.'
              : 'Reveal sent. It unlocks when they reveal too.',
        );
        ref.invalidate(conversationsProvider);
      case 'gift':
        if (conv?.peerRef != null) {
          await showGiftShop(context, ref, toRef: conv!.peerRef!);
        }
      case 'games':
        await showGamesSheet(
          context,
          matchId: widget.isGroup ? null : widget.convId,
          sendText: (text) =>
              ref.read(chatControllerProvider(_key).notifier).sendText(text),
        );
      case 'search':
        final id = await context.push<String>('/search?conv=${widget.convId}');
        if (id != null && mounted) {
          final msgs = ref.read(chatControllerProvider(_key)).value?.visible(DateTime.now()) ?? const [];
          await _jumpTo(id, msgs);
        }
      case 'verify':
        await _verify();
      case 'burn':
        await _pickBurn();
      case 'ideas':
        await _showIdeas();
      case 'wingman':
        await _askWingman();
      case 'wyr':
        context.push('/wyr/${widget.convId}');
      case 'mlt':
        context.push('/mlt/${widget.convId}');
      case 'party':
        context.push('/party/${widget.convId}');
      case 'delete':
        if (await _confirm('Delete this conversation for everyone?') &&
            mounted) {
          final ok = await guarded(
            context,
            () => ref
                .read(chatControllerProvider(_key).notifier)
                .deleteConversation(),
          );
          if (ok && mounted) context.pop();
        }
      case 'unmatch':
        if (await _confirm(
              'Unmatch? The chat will be removed for both of you.',
            ) &&
            mounted) {
          final ok = await guarded(context, () => repo.unmatch(widget.convId));
          if (ok && mounted) {
            ref.invalidate(conversationsProvider);
            context.pop();
          }
        }
      case 'block':
        if (conv?.peerRef != null &&
            await _confirm(
              'Block this person? You will not see each other again.',
            ) &&
            mounted) {
          final ok = await guarded(context, () => repo.block(conv!.peerRef!));
          if (ok && mounted) {
            ref.invalidate(conversationsProvider);
            context.pop();
          }
        }
      case 'report':
        await _report(conv);
      case 'plan':
        context.push('/safety?match=${widget.convId}');
    }
  }

  Future<T?> _try<T>(Future<T> Function() f) async {
    try {
      return await f();
    } catch (e) {
      if (mounted) showError(context, e);
      return null;
    }
  }

  Future<bool> _confirm(String msg) async =>
      await showDialog<bool>(
        context: context,
        builder: (c) => AlertDialog(
          content: Text(msg),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(c, false),
              child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(c, true),
              child: Text(AppLocalizations.of(context)?.sConfirm ?? 'Confirm'),
            ),
          ],
        ),
      ) ==
      true;

  Future<void> _pickBurn() async {
    final v = await pickBurnSeconds(context);
    if (v != null) ref.read(chatControllerProvider(_key).notifier).setBurn(v);
  }

  /// Opt-in: the member picks which recent messages go to the AI assistant.
  /// Chats are end-to-end encrypted, so nothing is shared unless they tap.
  Future<void> _askWingman() async {
    final recent = [
      for (final m in (ref.read(chatControllerProvider(_key)).value?.messages ?? const <ChatMessage>[]))
        if (m.kind == MsgKind.text && m.text.trim().isNotEmpty) m,
    ];
    final shown = recent.length > 8 ? recent.sublist(recent.length - 8) : recent;
    if (shown.isEmpty) {
      showMessage(context, 'Nothing to share yet. Send or receive a message first.');
      return;
    }
    final picked = await showModalBottomSheet<String>(
      context: context, useRootNavigator: true,
      isScrollControlled: true,
      builder: (c) => WingmanReplySheet(
        messages: shown,
        load: (lines) => ref.read(groupsRepositoryProvider).replyIdeas(widget.convId, lines),
      ),
    );
    if (picked != null && mounted) {
      _text.text = picked;
      _text.selection = TextSelection.collapsed(offset: picked.length);
    }
  }

  Future<void> _showIdeas() async {
    final s = await _try(
      () =>
          ref.read(groupsRepositoryProvider).suggestionsForMatch(widget.convId),
    );
    if (s == null || !mounted) return;
    await showModalBottomSheet<void>(
      context: context, useRootNavigator: true,
      builder: (c) => ListView(
        padding: const EdgeInsets.all(16),
        shrinkWrap: true,
        children: [
          Text('Icebreakers', style: Theme.of(context).textTheme.titleMedium),
          for (final i in s.icebreakers)
            ListTile(
              title: Text(i),
              onTap: () {
                _text.text = i;
                Navigator.pop(c);
              },
            ),
        ],
      ),
    );
  }

  /// A warning when the other person's latest messages look like a scam.
  /// Runs on this device on text it has already decrypted.
  List<Widget> _riskBanner(ChatState? s, Conversation? conv) {
    if (s == null || widget.isGroup) return const [];
    final recent = s.visible(DateTime.now()).where((m) => !m.fromMe && m.kind == MsgKind.text && m.text.isNotEmpty).toList();
    for (final m in recent.reversed.take(15)) {
      final risk = SafetyScan.check(m.text);
      if (risk == null) continue;
      if (_dismissedRisk == m.id) return const [];
      return [
        RiskBanner(
          risk: risk,
          onDismiss: () => setState(() => _dismissedRisk = m.id),
          onReport: () => _menu('report', conv),
          onBlock: () => _menu('block', conv),
          onAiCheck: () => _aiCheck(s),
        ),
      ];
    }
    return const [];
  }

  /// Sends the last few text messages to the AI service for a closer look, but
  /// only after saying so: chats are end-to-end encrypted and these lines are
  /// the one thing that leaves the phone. They are not stored.
  Future<void> _aiCheck(ChatState s) async {
    final yes = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Look closer?'),
        content: const Text('The last few text messages in this chat will be sent once to our AI to check for scam patterns. They are not saved and nobody else sees them. Nothing else from this chat is sent.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Cancel')),
          FilledButton(key: const Key('ai_confirm'), onPressed: () => Navigator.pop(c, true), child: const Text('Send these messages')),
        ],
      ),
    );
    if (yes != true || !mounted) return;
    final lines = [
      for (final m in s.visible(DateTime.now()).where((m) => m.kind == MsgKind.text && m.text.isNotEmpty).toList().reversed.take(8).toList().reversed)
        (fromMe: m.fromMe, text: m.text.length > 300 ? m.text.substring(0, 300) : m.text),
    ];
    ScamCheckResult? r;
    final ok = await guarded(context, () async => r = await ref.read(scamCheckRepositoryProvider).check(widget.convId, lines));
    if (!ok || r == null || !mounted) return;
    final res = r!;
    await showDialog<void>(
      context: context,
      builder: (c) => AlertDialog(
        key: const Key('ai_result'),
        title: Text(switch (res.risk) { 'high' => 'This looks risky', 'medium' => 'Be careful', _ => 'Nothing alarming found' }),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            for (final sg in res.signals) Padding(padding: const EdgeInsets.only(bottom: 6), child: Text('- ${sg.text}')),
            const SizedBox(height: 6),
            Text(res.advice),
            const SizedBox(height: 8),
            const Text('This is a guide, not proof. It can miss scams and flag honest people.', style: TextStyle(fontSize: 12)),
          ],
        ),
        actions: [TextButton(onPressed: () => Navigator.pop(c), child: const Text('Close'))],
      ),
    );
  }

  Future<void> _report(Conversation? conv) async {
    if (conv?.peerRef == null) return;
    final r = await pickReportReason(context);
    if (r == null || !mounted) return;
    final ok = await guarded(
      context,
      () => ref.read(discoveryRepositoryProvider).report(conv!.peerRef!, r),
    );
    if (ok && mounted) {
      showMessage(context, 'Thanks. Our team will review this.');
    }
  }

  @override
  Widget build(BuildContext context) {
    final conv = _conv();
    final chat = ref.watch(chatControllerProvider(_key));
    final ctrl = ref.read(chatControllerProvider(_key).notifier);
    final peer = widget.isGroup ? 'Group chat' : (conv?.title ?? 'Chat');

    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(conv?.title ?? peer),
            Text(
              chat.value?.peerTyping == true
                  ? 'typing…'
                  : 'End-to-end encrypted',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
        actions: [
          if (!widget.isGroup && conv?.randomExpiresAt == null) ...[
            IconButton(
              key: const Key('call_audio'),
              tooltip: 'Voice call',
              icon: const Icon(Icons.call_outlined),
              onPressed: () => _call(video: false),
            ),
            IconButton(
              key: const Key('call_video'),
              tooltip: 'Video call',
              icon: const Icon(Icons.videocam_outlined),
              onPressed: () => _call(video: true),
            ),
          ],
          PopupMenuButton<String>(
            key: const Key('chat_menu'),
            onSelected: (a) => _menu(a, conv),
            itemBuilder: (_) => [
              if (!widget.isGroup && conv?.anonymous == true)
                const PopupMenuItem(
                  value: 'reveal',
                  child: Text('Reveal myself'),
                ),
              if (!widget.isGroup && conv?.peerRef != null)
                const PopupMenuItem(value: 'gift', child: Text('Send a gift')),
              if (!widget.isGroup)
                const PopupMenuItem(value: 'verify', child: Text('Verify security')),
              const PopupMenuItem(key: Key('menu_search'), value: 'search', child: Text('Search in chat')),
              const PopupMenuItem(value: 'games', child: Text('Play a game')),
              const PopupMenuItem(
                value: 'burn',
                child: Text('Disappearing messages'),
              ),
              if (widget.isGroup)
                const PopupMenuItem(
                  value: 'ideas',
                  child: Text('Icebreaker ideas'),
                ),
              if (widget.isGroup)
                const PopupMenuItem(key: Key('play_party'), value: 'party', child: Text('Play Truth or Dare')),
              if (widget.isGroup)
                const PopupMenuItem(key: Key('play_mlt'), value: 'mlt', child: Text('Play Most likely to')),
              if (widget.isGroup)
                const PopupMenuItem(
                  key: Key('play_wyr'),
                  value: 'wyr',
                  child: Text('Play Would you rather'),
                ),
              if (widget.isGroup)
                const PopupMenuItem(
                  key: Key('ask_wingman'),
                  value: 'wingman',
                  child: Text('Ask Wingman for a reply'),
                ),
              const PopupMenuItem(
                value: 'delete',
                child: Text('Delete conversation'),
              ),
              if (!widget.isGroup)
                const PopupMenuItem(value: 'unmatch', child: Text('Unmatch')),
              if (!widget.isGroup && conv?.peerRef != null)
                const PopupMenuItem(value: 'block', child: Text('Block')),
              if (!widget.isGroup)
                const PopupMenuItem(key: Key('menu_plan'), value: 'plan', child: Text('Share my date plan')),
              if (!widget.isGroup && conv?.peerRef != null)
                const PopupMenuItem(value: 'report', child: Text('Report')),
            ],
          ),
        ],
      ),
      body: Column(
        children: [
          if (!widget.isGroup && conv != null && conv.anonymous) VoiceStepBanner(conv: conv),
          if (!widget.isGroup && conv != null && conv.anonymous) RevealTimerBanner(conv: conv),
          if (conv?.randomExpiresAt != null)
            RandomBanner(
              convId: widget.convId,
              expiresAt: conv!.randomExpiresAt!,
              keptByMe: conv.randomKeptByMe,
              onEnded: () {
                ref.invalidate(conversationsProvider);
                if (mounted) Navigator.of(context).maybePop();
              },
            ),
          if (chat.value?.identityChanged == true)
            Material(
              key: const Key('key_changed_banner'),
              color: Theme.of(context).colorScheme.errorContainer,
              child: ListTile(
                dense: true,
                leading: const Icon(Icons.gpp_maybe_outlined),
                title: const Text('Their security key changed'),
                subtitle: const Text('This can happen when they reinstall the app, or if someone is interfering. Check before you continue.'),
                trailing: TextButton(onPressed: _verify, child: Text(AppLocalizations.of(context)?.sReview ?? 'Review')),
              ),
            ),
          ..._riskBanner(chat.value, conv),
          if (conv != null && (conv.theirOpeningNote.isNotEmpty || conv.myOpeningNote.isNotEmpty))
            OpeningNotes(conv: conv),
          Expanded(
            child: chat.when(
              loading: () => const Loader(),
              error: (e, _) => ErrorView(
                error: e,
                onRetry: () => ref.invalidate(chatControllerProvider(_key)),
              ),
              data: (s) {
                final msgs = s.visible(DateTime.now());
                if (_pendingFocus != null) {
                  final id = _pendingFocus!;
                  _pendingFocus = null;
                  WidgetsBinding.instance.addPostFrameCallback((_) => _jumpTo(id, msgs));
                }
                if (msgs.isEmpty) {
                  return const EmptyState(
                    icon: Icons.lock_outline,
                    title: 'Say hello',
                    message: 'Messages are end-to-end encrypted. Only the two of you can read them.',
                  );
                }
                return RefreshIndicator(
                  onRefresh: ctrl.refresh,
                  child: ListView.builder(
                    controller: _scroll,
                    reverse: true,
                    physics: const AlwaysScrollableScrollPhysics(),
                    padding: const EdgeInsets.all(12),
                    itemCount: msgs.length,
                    itemBuilder: (c, i) {
                      final idx = msgs.length - 1 - i;
                      final m = msgs[idx];
                      final startsDay = idx == 0 || !_sameDay(msgs[idx - 1].createdAt, m.createdAt);
                      final bubble = MessageBubble(
                        key: ValueKey(m.id),
                        message: m,
                        loadImage: ctrl.openAttachment,
                        onLongPress: () => _messageMenu(m),
                        onDoubleTap: m.kind == MsgKind.locked || m.pending ? null : () => guarded(context, () => ctrl.react(m, '❤️')),
                      );
                      final shown = m.id == _highlight
                          ? KeyedSubtree(
                              key: _focusKey,
                              child: Container(
                                key: const Key('highlighted_message'),
                                decoration: BoxDecoration(color: Theme.of(context).colorScheme.tertiaryContainer.withValues(alpha: 0.6), borderRadius: BorderRadius.circular(14)),
                                child: bubble,
                              ),
                            )
                          : bubble;
                      if (!startsDay) return shown;
                      return Column(children: [_DayLabel(day: m.createdAt), shown]);
                    },
                  ),
                );
              },
            ),
          ),
          if ((chat.value?.burnAfterS ?? 0) > 0)
            Container(
              width: double.infinity,
              color: Theme.of(context).colorScheme.tertiaryContainer,
              padding: const EdgeInsets.symmetric(vertical: 4, horizontal: 12),
              child: Text(
                'Messages disappear ${chat.value!.burnAfterS}s after they are read',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ),
          if (_replyTo != null) _ReplyBar(message: _replyTo!, onClose: () => setState(() => _replyTo = null)),
          SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(8, 4, 12, 10),
              child: _recording
                  ? VoiceComposer(
                      onSend: (clip) => ref
                          .read(chatControllerProvider(_key).notifier)
                          .sendVoice(clip),
                      onClose: () {
                        if (mounted) setState(() => _recording = false);
                      },
                    )
                  : Row(
                      children: [
                        IconButton(
                          key: const Key('attach'),
                          tooltip: 'Photo',
                          onPressed: _attach,
                          icon: const Icon(Icons.image_outlined),
                        ),
                        IconButton(
                          key: const Key('gif'),
                          tooltip: 'GIF',
                          onPressed: _gif,
                          icon: const Icon(Icons.gif_box_outlined),
                        ),
                        Expanded(
                          child: TextField(
                            key: const Key('composer'),
                            controller: _text,
                            minLines: 1,
                            maxLines: 5,
                            textCapitalization: TextCapitalization.sentences,
                            decoration: const InputDecoration(
                              hintText: 'Message',
                              contentPadding: EdgeInsets.symmetric(
                                horizontal: 16,
                                vertical: 10,
                              ),
                            ),
                            onChanged: _onChanged,
                            onSubmitted: (_) => _send(),
                          ),
                        ),
                        if (_hasText)
                          IconButton.filled(
                            key: const Key('send'),
                            tooltip: 'Send',
                            style: _composerAction,
                            onPressed: _send,
                            icon: const Icon(Icons.arrow_upward_rounded),
                          )
                        else
                          IconButton.filled(
                            key: const Key('voice'),
                            tooltip: 'Record a voice note',
                            style: _composerAction,
                            onPressed: () => setState(() => _recording = true),
                            icon: const Icon(Icons.mic),
                          ),
                      ],
                    ),
            ),
          ),
        ],
      ),
    );
  }
}

/// How the match started: the notes each person sent with their like. They are
/// kept on the server (a match did not exist yet to carry an encrypted
/// message), so they are shown here, apart from the encrypted conversation.
class OpeningNotes extends StatelessWidget {
  const OpeningNotes({super.key, required this.conv});
  final Conversation conv;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    Widget quote(String who, String text) => Padding(
          padding: const EdgeInsets.only(top: 4),
          child: Text.rich(TextSpan(children: [
            TextSpan(text: '$who: ', style: t.textTheme.labelMedium),
            TextSpan(text: text, style: t.textTheme.bodyMedium?.copyWith(fontStyle: FontStyle.italic)),
          ])),
        );
    return Container(
      key: const Key('opening_notes'),
      width: double.infinity,
      margin: const EdgeInsets.fromLTRB(12, 8, 12, 0),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(color: t.colorScheme.primaryContainer.withValues(alpha: 0.35), borderRadius: BorderRadius.circular(14)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('How it started', style: t.textTheme.labelSmall?.copyWith(color: t.colorScheme.onSurfaceVariant)),
        if (conv.theirOpeningNote.isNotEmpty) quote(conv.title, conv.theirOpeningNote),
        if (conv.myOpeningNote.isNotEmpty) quote('You', conv.myOpeningNote),
      ]),
    );
  }
}

bool _sameDay(DateTime a, DateTime b) => a.year == b.year && a.month == b.month && a.day == b.day;

/// "Today", "Yesterday" or a date, above the first message of each day.
class _DayLabel extends StatelessWidget {
  const _DayLabel({required this.day});
  final DateTime day;

  @override
  Widget build(BuildContext context) {
    final now = DateTime.now();
    final text = _sameDay(day, now)
        ? 'Today'
        : _sameDay(day, now.subtract(const Duration(days: 1)))
            ? 'Yesterday'
            : '${day.day}/${day.month}/${day.year}';
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 10),
      child: Text(text, key: const Key('day_label'), style: Theme.of(context).textTheme.labelSmall?.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant)),
    );
  }
}

/// Above the composer while replying: what you are replying to, and a way out.
class _ReplyBar extends StatelessWidget {
  const _ReplyBar({required this.message, required this.onClose});
  final ChatMessage message;
  final VoidCallback onClose;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final quote = ChatController.excerpt(message);
    return Container(
      key: const Key('reply_bar'),
      color: t.colorScheme.surfaceContainerLow,
      padding: const EdgeInsets.fromLTRB(16, 8, 4, 8),
      child: Row(children: [
        Container(width: 3, height: 34, color: t.colorScheme.primary),
        const SizedBox(width: 10),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(message.fromMe ? 'Replying to yourself' : 'Replying', style: t.textTheme.labelSmall?.copyWith(color: t.colorScheme.primary)),
            Text(quote, maxLines: 1, overflow: TextOverflow.ellipsis, style: t.textTheme.bodySmall),
          ]),
        ),
        IconButton(key: const Key('reply_cancel'), onPressed: onClose, icon: const Icon(Icons.close)),
      ]),
    );
  }
}
