import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../chat/data/chat_repository.dart';
import '../random_repository.dart';

/// Random chat: tap once to be paired with someone who is also waiting. Both
/// of you are anonymous, the chat is end-to-end encrypted, and it ends after
/// ten minutes unless you both choose to keep it.
class RandomScreen extends ConsumerStatefulWidget {
  const RandomScreen({
    super.key,
    this.pollInterval = const Duration(seconds: 2),
    this.style = 'chat',
  });
  final Duration pollInterval;

  /// 'truth' is Masked Truth: the same anonymous pairing, opened as truth or dare.
  final String style;

  @override
  ConsumerState<RandomScreen> createState() => _RandomScreenState();
}

class _RandomScreenState extends ConsumerState<RandomScreen>
    with SingleTickerProviderStateMixin {
  late final RandomRepository _repo;
  late final AnimationController _pulse = AnimationController(
    vsync: this,
    duration: const Duration(seconds: 3),
  );
  Timer? _timer;
  RandomState _state = RandomState.idle;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(randomRepositoryProvider);
    _resume();
  }

  /// If a random chat is already running (or we are still queued from before),
  /// pick it up instead of showing the start button.
  Future<void> _resume() async {
    try {
      _apply(await _repo.status());
    } catch (_) {
      // stays idle; the start button reports any real error
    }
  }

  void _apply(RandomStatus s) {
    if (!mounted) return;
    if (s.state == RandomState.matched && s.session != null) {
      _timer?.cancel();
      _state = RandomState.matched; // so dispose() does not cancel a queue entry that is already used up
      ref.invalidate(conversationsProvider);
      context.pushReplacement(
        '/chats/${s.session!.matchId}${s.session!.isTruth ? '?game=1' : ''}',
      );
      return;
    }
    setState(() => _state = s.state);
    if (s.state == RandomState.waiting) {
      if (!_pulse.isAnimating) _pulse.repeat();
      _timer ??= Timer.periodic(widget.pollInterval, (_) => _poll());
    } else {
      _pulse.stop();
      _timer?.cancel();
      _timer = null;
    }
  }

  Future<void> _poll() async {
    try {
      _apply(await _repo.status());
    } catch (_) {
      // transient; the next tick retries
    }
  }

  Future<void> _start() async {
    setState(() => _busy = true);
    try {
      _apply(await _repo.join(style: widget.style));
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _cancel() async {
    _timer?.cancel();
    _timer = null;
    await guarded(context, _repo.cancel);
    if (mounted) setState(() => _state = RandomState.idle);
    _pulse.stop();
  }

  @override
  void dispose() {
    _timer?.cancel();
    _pulse.dispose();
    // Leaving the screen while queued leaves the queue, so nobody is paired
    // with a person who has already walked away.
    if (_state == RandomState.waiting) _repo.cancel().ignore();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final waiting = _state == RandomState.waiting;
    final truth = widget.style == 'truth';
    return Scaffold(
      appBar: AppBar(title: Text(truth ? 'Masked Truth' : 'Random chat')),
      body: Padding(
        padding: const EdgeInsets.fromLTRB(24, 8, 24, 24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Expanded(
              // Scrolls (and stays centred when it fits) so small screens and
              // large text sizes never overflow.
              child: LayoutBuilder(
                builder: (context, box) => SingleChildScrollView(
                  child: ConstrainedBox(
                    constraints: BoxConstraints(minHeight: box.maxHeight),
                    child: Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          _Aura(controller: _pulse, active: waiting),
                          const SizedBox(height: 32),
                          Text(
                            waiting
                                ? 'Looking for someone'
                                : (truth
                                      ? 'Truth or dare, masked'
                                      : 'Talk to someone new'),
                            key: const Key('random_title'),
                            style: t.textTheme.headlineMedium,
                            textAlign: TextAlign.center,
                          ),
                          const SizedBox(height: 12),
                          Text(
                            waiting
                                ? 'Stay on this screen. We will open the chat the moment someone is ready.'
                                : (truth
                                      ? 'You are paired with another masked player. Take turns drawing a truth or a dare. '
                                            'Nobody shows a name or photo, and the chat is end-to-end encrypted.'
                                      : 'You are matched with another person who is waiting right now. '
                                            'Neither of you shows a name or photo, and the chat is end-to-end encrypted.'),
                            style: t.textTheme.bodyLarge?.copyWith(
                              color: t.colorScheme.onSurfaceVariant,
                            ),
                            textAlign: TextAlign.center,
                          ),
                          if (!waiting) ...[
                            const SizedBox(height: 20),
                            Text(
                              'It lasts ten minutes. If you both choose Keep chatting, it stays. Otherwise it disappears, including the messages.',
                              style: t.textTheme.bodyMedium?.copyWith(
                                color: t.colorScheme.onSurfaceVariant,
                              ),
                              textAlign: TextAlign.center,
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                ),
              ),
            ),
            if (waiting)
              OutlinedButton(
                key: const Key('random_cancel'),
                onPressed: _cancel,
                child: const Text('Stop looking'),
              )
            else
              FilledButton(
                key: const Key('random_start'),
                onPressed: _busy ? null : _start,
                child: _busy
                    ? const SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : Text(truth ? 'Find a player' : 'Start random chat'),
              ),
            const SizedBox(height: 12),
            Text(
              'Be kind. You can leave, block or report at any time.',
              style: t.textTheme.bodySmall,
              textAlign: TextAlign.center,
            ),
          ],
        ),
      ),
    );
  }
}

/// A Dusk-coloured orb that breathes while searching.
class _Aura extends StatelessWidget {
  const _Aura({required this.controller, required this.active});
  final AnimationController controller;
  final bool active;

  @override
  Widget build(BuildContext context) {
    const light = ModeLight.dusk;
    return AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        final v = active
            ? (0.5 - (controller.value - 0.5).abs()) * 2
            : 0.0; // 0..1..0
        return SizedBox(
          width: 190,
          height: 190,
          child: Stack(
            alignment: Alignment.center,
            children: [
              for (final r in [1.0, 0.8])
                Opacity(
                  opacity: active ? 0.18 + 0.12 * v : 0.12,
                  child: Container(
                    width: 190 * r * (1 + 0.08 * v),
                    height: 190 * r * (1 + 0.08 * v),
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      gradient: light.gradient,
                    ),
                  ),
                ),
              Container(
                width: 104,
                height: 104,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  gradient: light.gradient,
                  boxShadow: [
                    BoxShadow(
                      color: light.solid.withValues(alpha: 0.45),
                      blurRadius: 28,
                      offset: const Offset(0, 10),
                    ),
                  ],
                ),
                child: const Icon(Icons.shuffle, color: Colors.white, size: 38),
              ),
            ],
          ),
        );
      },
    );
  }
}
