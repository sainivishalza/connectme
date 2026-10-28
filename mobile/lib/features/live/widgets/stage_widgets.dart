import 'package:flutter/material.dart';

import '../live_repository.dart';

/// A soft colour for a person's avatar, the same for the same name.
Color _tint(String name, ColorScheme cs) {
  const hues = [Color(0xFFE57373), Color(0xFFBA68C8), Color(0xFF64B5F6), Color(0xFF4DB6AC), Color(0xFFFFB74D), Color(0xFF81C784), Color(0xFFF06292)];
  final i = name.runes.fold<int>(0, (a, r) => a + r) % hues.length;
  return Color.alphaBlend(hues[i].withValues(alpha: 0.85), cs.surface);
}

String _initial(String name) => name.trim().isEmpty ? '?' : String.fromCharCode(name.trim().runes.first).toUpperCase();

/// The host's list of raised hands: who is asking, with a clear Let up and Not
/// now for each person, and how many of the guest places are taken.
class StageRequestsSheet extends StatelessWidget {
  const StageRequestsSheet({super.key, required this.state, required this.onDecide});
  final StageState state;
  final Future<void> Function(StageAsk ask, bool accept) onDecide;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final cs = t.colorScheme;
    final used = state.max == 0 ? 0.0 : (state.guests / state.max).clamp(0.0, 1.0);
    final full = state.guests >= state.max;
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(maxHeight: MediaQuery.of(context).size.height * 0.75),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(children: [
                Expanded(child: Text('Asking to join', style: t.textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700))),
                if (state.requests.isNotEmpty)
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                    decoration: BoxDecoration(color: cs.primaryContainer, borderRadius: BorderRadius.circular(20)),
                    child: Text('${state.requests.length}', style: TextStyle(color: cs.onPrimaryContainer, fontWeight: FontWeight.w700)),
                  ),
              ]),
              const SizedBox(height: 10),
              ClipRRect(
                borderRadius: BorderRadius.circular(8),
                child: LinearProgressIndicator(value: used, minHeight: 8, backgroundColor: cs.surfaceContainerHighest),
              ),
              const SizedBox(height: 6),
              Text(
                full ? 'The stage is full (${state.guests} of ${state.max} guest places)' : '${state.guests} of ${state.max} guest places used',
                style: t.textTheme.bodySmall?.copyWith(color: cs.onSurfaceVariant),
              ),
              const SizedBox(height: 14),
              if (state.requests.isEmpty)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 28),
                  child: Center(
                    child: Column(mainAxisSize: MainAxisSize.min, children: [
                      Icon(Icons.pan_tool_outlined, size: 40, color: cs.onSurfaceVariant),
                      const SizedBox(height: 10),
                      const Text('No one is waiting'),
                      const SizedBox(height: 4),
                      Text('When someone raises a hand it appears here.', style: t.textTheme.bodySmall?.copyWith(color: cs.onSurfaceVariant)),
                    ]),
                  ),
                )
              else
                Flexible(
                  child: ListView.separated(
                    shrinkWrap: true,
                    itemCount: state.requests.length,
                    separatorBuilder: (_, _) => const SizedBox(height: 10),
                    itemBuilder: (c, i) => _RequestCard(ask: state.requests[i], full: full, onDecide: onDecide),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _RequestCard extends StatelessWidget {
  const _RequestCard({required this.ask, required this.full, required this.onDecide});
  final StageAsk ask;
  final bool full;
  final Future<void> Function(StageAsk ask, bool accept) onDecide;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final cs = t.colorScheme;
    return Container(
      key: Key('ask_${ask.ref}'),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(color: cs.surfaceContainerHigh, borderRadius: BorderRadius.circular(18)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          CircleAvatar(radius: 22, backgroundColor: _tint(ask.name, cs), child: Text(_initial(ask.name), style: const TextStyle(color: Colors.white, fontWeight: FontWeight.w700, fontSize: 18))),
          const SizedBox(width: 12),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(ask.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: t.textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w600)),
              Row(children: [
                Icon(Icons.pan_tool, size: 13, color: cs.primary),
                const SizedBox(width: 4),
                Text('Raised a hand', style: t.textTheme.bodySmall?.copyWith(color: cs.onSurfaceVariant)),
              ]),
            ]),
          ),
        ]),
        const SizedBox(height: 12),
        Row(children: [
          Expanded(
            child: OutlinedButton(
              key: Key('decline_req_${ask.ref}'),
              onPressed: () => onDecide(ask, false),
              child: const Text('Not now'),
            ),
          ),
          const SizedBox(width: 10),
          Expanded(
            flex: 2,
            child: FilledButton.icon(
              key: Key('accept_req_${ask.ref}'),
              onPressed: full ? null : () => onDecide(ask, true),
              icon: const Icon(Icons.videocam, size: 18),
              label: const Text('Let up'),
            ),
          ),
        ]),
      ]),
    );
  }
}

/// What a viewer sees when the host invites them onto camera: clear about what
/// happens, with a big Join and a quiet Not now.
class StageInviteCard extends StatelessWidget {
  const StageInviteCard({super.key, required this.onJoin, required this.onDecline});
  final VoidCallback onJoin;
  final VoidCallback onDecline;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final cs = t.colorScheme;
    return Material(
      key: const Key('stage_invite'),
      elevation: 6,
      color: cs.surface,
      borderRadius: BorderRadius.circular(22),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 14, 16, 12),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            CircleAvatar(radius: 20, backgroundColor: cs.primaryContainer, child: Icon(Icons.videocam, color: cs.onPrimaryContainer)),
            const SizedBox(width: 12),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text('You are invited on camera', style: t.textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w700)),
                Text('The host would like you to join. Everyone in the room will see and hear you.',
                    style: t.textTheme.bodySmall?.copyWith(color: cs.onSurfaceVariant)),
              ]),
            ),
          ]),
          const SizedBox(height: 12),
          Row(children: [
            Expanded(child: OutlinedButton(key: const Key('decline_invite'), onPressed: onDecline, child: const Text('Not now'))),
            const SizedBox(width: 10),
            Expanded(flex: 2, child: FilledButton.icon(key: const Key('accept_invite'), onPressed: onJoin, icon: const Icon(Icons.videocam, size: 18), label: const Text('Join on camera'))),
          ]),
        ]),
      ),
    );
  }
}
