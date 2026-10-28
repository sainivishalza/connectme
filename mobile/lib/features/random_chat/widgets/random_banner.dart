import 'dart:async';

import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../chat/data/chat_repository.dart';
import '../random_repository.dart';

/// Shown above an anonymous random chat: how long is left, and the choice to
/// keep talking. Both people must choose "Keep chatting"; otherwise the chat
/// and its messages disappear when the time runs out.
class RandomBanner extends ConsumerStatefulWidget {
  const RandomBanner({
    super.key,
    required this.convId,
    required this.expiresAt,
    required this.keptByMe,
    required this.onEnded,
    this.now = DateTime.now,
  });

  final String convId;
  final DateTime expiresAt;
  final bool keptByMe;
  final VoidCallback onEnded;

  /// The clock; tests replace it because widget tests do not fake wall time.
  final DateTime Function() now;

  @override
  ConsumerState<RandomBanner> createState() => _RandomBannerState();
}

class _RandomBannerState extends ConsumerState<RandomBanner> {
  Timer? _tick;
  bool _ended = false;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _tick = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      if (_left() <= Duration.zero && !_ended) {
        _ended = true;
        _tick?.cancel();
        showMessage(context, 'The anonymous chat ended.');
        widget.onEnded();
        return;
      }
      setState(() {});
    });
  }

  @override
  void dispose() {
    _tick?.cancel();
    super.dispose();
  }

  Duration _left() => widget.expiresAt.difference(widget.now());

  String _clock(Duration d) {
    final s = d.inSeconds.clamp(0, 86400);
    return '${(s ~/ 60).toString().padLeft(2, '0')}:${(s % 60).toString().padLeft(2, '0')}';
  }

  Future<void> _keep() async {
    setState(() => _busy = true);
    final ok = await guarded(
      context,
      () => ref.read(randomRepositoryProvider).keep(widget.convId),
    );
    if (ok) ref.invalidate(conversationsProvider);
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final light = ModeLight.dusk;
    final left = _left();
    final urgent = left < const Duration(minutes: 1);
    return Container(
      key: const Key('random_banner'),
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(20, 12, 12, 12),
      decoration: BoxDecoration(
        gradient: light.wash(t.brightness),
        border: Border(
          bottom: BorderSide(color: light.solid.withValues(alpha: 0.25)),
        ),
      ),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Anonymous chat ends in ${_clock(left)}',
                  style: t.textTheme.labelLarge?.copyWith(
                    color: urgent ? t.colorScheme.error : null,
                  ),
                ),
                Text(
                  widget.keptByMe
                      ? 'Waiting for them to keep it too.'
                      : 'It stays only if you both keep chatting.',
                  style: t.textTheme.bodySmall,
                ),
              ],
            ),
          ),
          if (!widget.keptByMe)
            FilledButton(
              key: const Key('random_keep'),
              onPressed: _busy ? null : _keep,
              style: FilledButton.styleFrom(
                minimumSize: const Size(0, 40),
                padding: const EdgeInsets.symmetric(horizontal: 18),
              ),
              child: Text(AppLocalizations.of(context)?.sKeepChatting ?? 'Keep chatting'),
            ),
        ],
      ),
    );
  }
}
