import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_snack.dart';
import '../games_repository.dart';

/// "Play a game" from a chat: truth, dare, an icebreaker, or the couple quiz
/// (one-to-one chats only). A drawn prompt can be sent straight into the
/// conversation, where it is end-to-end encrypted like any message.
Future<void> showGamesSheet(
  BuildContext context, {
  required String? matchId,
  required Future<void> Function(String text) sendText,
}) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (c) => _GamesSheet(matchId: matchId, sendText: sendText),
  );
}

class _GamesSheet extends ConsumerStatefulWidget {
  const _GamesSheet({required this.matchId, required this.sendText});
  final String? matchId;
  final Future<void> Function(String text) sendText;

  @override
  ConsumerState<_GamesSheet> createState() => _GamesSheetState();
}

class _GamesSheetState extends ConsumerState<_GamesSheet> {
  GamePrompt? _prompt;
  bool _busy = false;

  static const _labels = {'truth': 'Truth', 'dare': 'Dare', 'icebreaker': 'Icebreaker'};

  Future<void> _draw(String kind) async {
    setState(() => _busy = true);
    try {
      final p = await ref.read(gamesRepositoryProvider).prompt(kind);
      if (mounted) setState(() => _prompt = p);
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _send() async {
    final p = _prompt;
    if (p == null) return;
    final nav = Navigator.of(context);
    final ok = await guarded(context, () => widget.sendText('${_labels[p.kind]}: ${p.text}'));
    if (ok) nav.pop();
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final p = _prompt;
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.fromLTRB(20, 0, 20, 20 + MediaQuery.viewInsetsOf(context).bottom),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('Play a game', style: t.textTheme.headlineSmall),
              const SizedBox(height: 4),
              Text('Pick one and send it to the chat.', style: t.textTheme.bodyMedium?.copyWith(color: t.colorScheme.onSurfaceVariant)),
              const SizedBox(height: 16),
              Wrap(spacing: 10, runSpacing: 10, children: [
                for (final e in _labels.entries)
                  ChoiceChip(
                    key: Key('game_${e.key}'),
                    label: Text(e.value),
                    selected: p?.kind == e.key,
                    onSelected: _busy ? null : (_) => _draw(e.key),
                  ),
              ]),
              if (p != null) ...[
                const SizedBox(height: 18),
                Container(
                  key: const Key('game_prompt'),
                  padding: const EdgeInsets.all(20),
                  decoration: BoxDecoration(
                    gradient: ModeLight.daylight.wash(t.brightness),
                    borderRadius: BorderRadius.circular(Radii.card),
                  ),
                  child: Text(p.text, style: t.textTheme.titleLarge),
                ),
                const SizedBox(height: 12),
                Row(children: [
                  Expanded(
                    child: OutlinedButton(
                      key: const Key('game_another'),
                      onPressed: _busy ? null : () => _draw(p.kind),
                      child: const Text('Another one'),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(child: FilledButton(key: const Key('game_send'), onPressed: _send, child: const Text('Send to chat'))),
                ]),
              ],
              if (widget.matchId != null) ...[
                const SizedBox(height: 22),
                const Divider(),
                const SizedBox(height: 10),
                ListTile(
                  key: const Key('game_quiz'),
                  contentPadding: EdgeInsets.zero,
                  leading: const Icon(Icons.quiz_outlined),
                  title: const Text('Couple quiz'),
                  subtitle: const Text('Answer the same questions, then compare. Nothing is shown until you both finish.'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () {
                    final id = widget.matchId!;
                    Navigator.of(context).pop();
                    context.push('/quiz/$id');
                  },
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
