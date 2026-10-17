import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_snack.dart';
import '../calls_repository.dart';

/// Full-screen "someone is calling" with Accept and Decline.
class IncomingCallScreen extends ConsumerStatefulWidget {
  const IncomingCallScreen({super.key, required this.call});
  final CallInfo call;

  @override
  ConsumerState<IncomingCallScreen> createState() => _IncomingCallScreenState();
}

class _IncomingCallScreenState extends ConsumerState<IncomingCallScreen> {
  bool _busy = false;

  Future<void> _accept() async {
    setState(() => _busy = true);
    try {
      final started = await ref.read(callsRepositoryProvider).accept(widget.call.id);
      if (mounted) context.pushReplacement('/call', extra: started);
    } catch (e) {
      if (mounted) {
        showError(context, e);
        Navigator.of(context).maybePop();
      }
    }
  }

  Future<void> _decline() async {
    setState(() => _busy = true);
    await guarded(context, () => ref.read(callsRepositoryProvider).decline(widget.call.id));
    if (mounted) Navigator.of(context).maybePop();
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final c = widget.call;
    return Scaffold(
      backgroundColor: Palette.midnight,
      body: Container(
        decoration: BoxDecoration(gradient: ModeLight.dusk.gradient),
        child: SafeArea(
          child: Padding(
            padding: const EdgeInsets.all(28),
            child: Column(
              children: [
                const Spacer(),
                const Icon(Icons.person_outline, size: 84, color: Colors.white70),
                const SizedBox(height: 18),
                Text(c.peer?.title ?? 'Anonymous', key: const Key('incoming_name'), style: t.textTheme.displaySmall?.copyWith(color: Colors.white), textAlign: TextAlign.center),
                const SizedBox(height: 10),
                Text(c.video ? 'Incoming video call' : 'Incoming voice call', style: t.textTheme.titleMedium?.copyWith(color: Colors.white70)),
                const Spacer(),
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                  children: [
                    _Answer(key: const Key('call_decline'), icon: Icons.call_end, label: 'Decline', color: Palette.danger, onTap: _busy ? null : _decline),
                    _Answer(key: const Key('call_accept'), icon: c.video ? Icons.videocam : Icons.call, label: 'Accept', color: const Color(0xFF1F9D63), onTap: _busy ? null : _accept),
                  ],
                ),
                const SizedBox(height: 24),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _Answer extends StatelessWidget {
  const _Answer({super.key, required this.icon, required this.label, required this.color, required this.onTap});
  final IconData icon;
  final String label;
  final Color color;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Material(
          color: onTap == null ? color.withValues(alpha: 0.5) : color,
          shape: const CircleBorder(),
          child: InkWell(
            customBorder: const CircleBorder(),
            onTap: onTap,
            child: SizedBox(width: 72, height: 72, child: Icon(icon, color: Colors.white, size: 32)),
          ),
        ),
        const SizedBox(height: 8),
        Text(label, style: Theme.of(context).textTheme.labelLarge?.copyWith(color: Colors.white)),
      ],
    );
  }
}
