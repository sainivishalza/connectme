import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models.dart';
import '../../../core/providers.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../chat/voice/voice_bubble.dart';
import '../../chat/voice/voice_composer.dart';
import '../../chat/voice/voice_io.dart';
import '../profile_repository.dart';

/// Record, hear and remove your voice intro: a short spoken hello that people
/// who can see your full profile may play. Anonymous profiles never expose it.
class VoiceIntroSection extends ConsumerStatefulWidget {
  const VoiceIntroSection({super.key, required this.profile});
  final OwnProfile profile;

  @override
  ConsumerState<VoiceIntroSection> createState() => _VoiceIntroSectionState();
}

class _VoiceIntroSectionState extends ConsumerState<VoiceIntroSection> {
  bool _recording = false;

  static const maxSeconds = 30;

  Future<void> _upload(VoiceClip clip) async {
    await ref.read(profileRepositoryProvider).setVoiceIntro(clip.bytes, clip.seconds);
    ref.invalidate(meProvider);
    if (mounted) showMessage(context, 'Voice intro saved');
  }

  Future<void> _remove() async {
    final ok = await guarded(context, () => ref.read(profileRepositoryProvider).deleteVoiceIntro());
    if (ok) ref.invalidate(meProvider);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final p = widget.profile;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Voice intro', style: t.textTheme.titleMedium),
        const SizedBox(height: 4),
        Text(
          'A hello of up to 30 seconds. Only people who can see your full profile can hear it; it stays hidden while you are anonymous.',
          style: t.textTheme.bodySmall,
        ),
        const SizedBox(height: 10),
        if (_recording)
          VoiceComposer(
            maxSeconds: maxSeconds,
            onSend: _upload,
            onClose: () {
              if (mounted) setState(() => _recording = false);
            },
          )
        else if (p.voiceIntroS > 0)
          Row(
            children: [
              VoicePlayback(
                seconds: p.voiceIntroS,
                mime: 'audio/mp4',
                color: t.colorScheme.onSurface,
                load: () => ref.read(apiClientProvider).getBytes('/media/voice/${p.ref}'),
              ),
              const Spacer(),
              TextButton(key: const Key('rerecord_voice_intro'), onPressed: () => setState(() => _recording = true), child: Text(AppLocalizations.of(context)?.sRecordAgain ?? 'Record again')),
              IconButton(key: const Key('delete_voice_intro'), tooltip: 'Remove voice intro', onPressed: _remove, icon: const Icon(Icons.delete_outline)),
            ],
          )
        else
          OutlinedButton.icon(
            key: const Key('record_voice_intro'),
            onPressed: () => setState(() => _recording = true),
            icon: const Icon(Icons.mic_none),
            label: Text(AppLocalizations.of(context)?.sRecordAVoiceIntro ?? 'Record a voice intro'),
          ),
      ],
    );
  }
}
