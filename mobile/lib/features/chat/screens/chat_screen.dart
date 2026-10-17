import 'dart:async';

import 'package:flutter/material.dart';

import '../../random_chat/widgets/random_banner.dart';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:image_picker/image_picker.dart';

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
import '../widgets/message_bubble.dart';

class ChatScreen extends ConsumerStatefulWidget {
  const ChatScreen({
    super.key,
    required this.convId,
    required this.isGroup,
    this.openGames = false,
  });
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
  Timer? _tick;
  Timer? _typingDebounce;
  bool _typingSent = false;

  ConvKey get _key => (id: widget.convId, group: widget.isGroup);

  @override
  void initState() {
    super.initState();
    // Programmatic changes (icebreaker ideas, clearing after send) must update the
    // send/mic button too, not only typing.
    _text.addListener(() {
      final has = _text.text.trim().isNotEmpty;
      if (has != _hasText && mounted) setState(() => _hasText = has);
    });
    if (widget.openGames) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _menu('games', null);
      });
    }
    // Re-evaluates burn timers.
    _tick = Timer.periodic(const Duration(seconds: 2), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
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
    _text.clear();
    await guarded(
      context,
      () => ref.read(chatControllerProvider(_key).notifier).sendText(t),
    );
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
        title: const Text('Verify security'),
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
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Close')),
          if (changed) FilledButton(key: const Key('trust_new_key'), onPressed: () => Navigator.pop(c, true), child: const Text('Trust the new key')),
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
      case 'verify':
        await _verify();
      case 'burn':
        await _pickBurn();
      case 'ideas':
        await _showIdeas();
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
              child: const Text('Cancel'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(c, true),
              child: const Text('Confirm'),
            ),
          ],
        ),
      ) ==
      true;

  Future<void> _pickBurn() async {
    final v = await pickBurnSeconds(context);
    if (v != null) ref.read(chatControllerProvider(_key).notifier).setBurn(v);
  }

  Future<void> _showIdeas() async {
    final s = await _try(
      () =>
          ref.read(groupsRepositoryProvider).suggestionsForMatch(widget.convId),
    );
    if (s == null || !mounted) return;
    await showModalBottomSheet<void>(
      context: context,
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
              const PopupMenuItem(
                value: 'delete',
                child: Text('Delete conversation'),
              ),
              if (!widget.isGroup)
                const PopupMenuItem(value: 'unmatch', child: Text('Unmatch')),
              if (!widget.isGroup && conv?.peerRef != null)
                const PopupMenuItem(value: 'block', child: Text('Block')),
              if (!widget.isGroup && conv?.peerRef != null)
                const PopupMenuItem(value: 'report', child: Text('Report')),
            ],
          ),
        ],
      ),
      body: Column(
        children: [
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
                trailing: TextButton(onPressed: _verify, child: const Text('Review')),
              ),
            ),
          Expanded(
            child: chat.when(
              loading: () => const Loader(),
              error: (e, _) => ErrorView(
                error: e,
                onRetry: () => ref.invalidate(chatControllerProvider(_key)),
              ),
              data: (s) {
                final msgs = s.visible(DateTime.now());
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
                    reverse: true,
                    physics: const AlwaysScrollableScrollPhysics(),
                    padding: const EdgeInsets.all(12),
                    itemCount: msgs.length,
                    itemBuilder: (c, i) {
                      final m = msgs[msgs.length - 1 - i];
                      return MessageBubble(
                        key: ValueKey(m.id),
                        message: m,
                        loadImage: ctrl.openAttachment,
                      );
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
          SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(8, 4, 8, 8),
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
                            onPressed: _send,
                            icon: const Icon(Icons.send),
                          )
                        else
                          IconButton.filled(
                            key: const Key('voice'),
                            tooltip: 'Record a voice note',
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
