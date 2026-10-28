import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../discovery/discovery_repository.dart';
import '../data/chat_models.dart';
import '../data/chat_repository.dart';
import '../voice/voice_bubble.dart';

/// The voice step of the staged reveal in an anonymous chat: swap voice intros
/// first, photos and names later. Each side agrees on their own; the clip
/// plays only once both have.
class VoiceStepBanner extends ConsumerWidget {
  const VoiceStepBanner({super.key, required this.conv});
  final Conversation conv;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final step = conv.voiceStep;
    if (step == null) return const SizedBox.shrink();
    final t = Theme.of(context);
    Widget body;
    if (step.unlocked) {
      body = Row(children: [
        const Expanded(child: Text('Their voice intro is unlocked')),
        step.seconds > 0
            ? VoicePlayback(
                key: const Key('match_voice'),
                seconds: step.seconds,
                mime: 'audio/mp4',
                color: t.colorScheme.onSurface,
                load: () => ref.read(apiClientProvider).getBytes('/matching/matches/${conv.id}/voice'),
              )
            : const Text('They have not recorded one yet'),
      ]);
    } else if (step.byMe) {
      body = const Text('Waiting for them to agree. You will both hear each other when they do.', key: Key('voice_waiting'));
    } else {
      body = Row(children: [
        const Expanded(child: Text('Hear each other\'s voice before photos')),
        FilledButton.tonal(
          key: const Key('voice_step_ask'),
          onPressed: () async {
            final ok = await guarded(context, () => ref.read(discoveryRepositoryProvider).revealVoice(conv.id));
            if (ok) ref.invalidate(conversationsProvider);
          },
          child: Text(AppLocalizations.of(context)?.sSwapVoices ?? 'Swap voices'),
        ),
      ]);
    }
    return Material(
      key: const Key('voice_step'),
      color: t.colorScheme.secondaryContainer,
      child: Padding(padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10), child: body),
    );
  }
}
