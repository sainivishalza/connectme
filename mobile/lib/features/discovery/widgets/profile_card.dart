import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models.dart';
import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../chat/voice/voice_bubble.dart';
import '../../hotel_mode/moods.dart';
import '../../chapters/widgets/chapters_section.dart';
import '../../videos/videos_repository.dart';
import '../../videos/widgets/video_view.dart';
import '../../profile/widgets/detail_widgets.dart';
import 'photo_carousel.dart';

/// A person as the server chose to show them. Anonymous profiles never show a
/// photo or name; the card says so rather than hiding it, and shows them
/// behind the light of their mode instead of a grey placeholder.
class ProfileCard extends StatelessWidget {
  const ProfileCard({super.key, required this.profile, this.onLike, this.onPass, this.onSuperLike, this.onTap});

  final PublicProfile profile;
  final VoidCallback? onLike;
  final VoidCallback? onPass;
  final VoidCallback? onSuperLike;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final p = profile;
    final light = ModeLight.of(p.anonymous && p.mode == 'open' ? 'incognito' : p.mode);
    return Card(
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(Radii.hero),
        side: BorderSide(color: t.colorScheme.outlineVariant),
      ),
      child: InkWell(
        onTap: onTap,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _Stage(profile: p, light: light),
            Padding(
              padding: const EdgeInsets.fromLTRB(22, 16, 22, 18),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(children: [
                    Flexible(child: Text(p.title, style: t.textTheme.headlineMedium, overflow: TextOverflow.ellipsis)),
                    if (p.idVerified) ...[const SizedBox(width: 8), const VerifiedBadge()],
                    if (p.charmLevel >= 2) ...[const SizedBox(width: 8), CharmBadge(level: p.charmLevel)],
                    if (p.vip) ...[
                      const SizedBox(width: 8),
                      Icon(Icons.workspace_premium, color: t.colorScheme.tertiary, size: 24),
                    ],
                  ]),
                  const SizedBox(height: 10),
                  Wrap(spacing: 8, runSpacing: 8, children: [
                    if (p.quizMatch > 0) QuizMatchBadge(percent: p.quizMatch),
                    if (p.age != null) _Fact(icon: Icons.cake_outlined, text: '${p.age}'),
                    if (p.distance.isNotEmpty) _Fact(icon: Icons.near_me_outlined, text: p.distance),
                    if (p.hotelName.isNotEmpty) _Fact(icon: Icons.hotel_outlined, text: p.hotelName),
                    if (p.travelMood.isNotEmpty) _Fact(icon: Icons.luggage_outlined, text: moodLabel(p.travelMood)),
                    if (p.anonymous)
                      Chip(
                        label: Text(AppLocalizations.of(context)?.sAnonymous ?? 'Anonymous'),
                        avatar: const Icon(Icons.lock_outline, size: 16),
                        visualDensity: VisualDensity.compact,
                      ),
                  ]),
                  if (p.intro.isNotEmpty) ...[const SizedBox(height: 16), Text(p.intro, style: t.textTheme.bodyLarge)],
                  if (p.details != null && p.details!.facts.isNotEmpty) ...[
                    const SizedBox(height: 14),
                    DetailChips(key: const Key('detail_chips'), facts: p.details!.facts),
                  ],
                  for (final pr in p.prompts) ...[const SizedBox(height: 12), PromptBlock(prompt: pr)],
                  if (p.voiceIntroS > 0 && !p.anonymous) ...[const SizedBox(height: 12), _VoiceIntro(profile: p)],
                  if (p.reelS > 0 && !p.anonymous) _ReelButton(profile: p),
                  if (p.chapters > 0 && !p.anonymous) _ChaptersButton(profile: p),
                  if (p.bio.isNotEmpty) ...[
                    const SizedBox(height: 8),
                    Text(p.bio, maxLines: 3, overflow: TextOverflow.ellipsis, style: t.textTheme.bodyMedium?.copyWith(color: t.colorScheme.onSurfaceVariant)),
                  ],
                  if (p.mood.isNotEmpty) ...[const SizedBox(height: 8), Text('Mood: ${p.mood}', style: t.textTheme.bodySmall)],
                  if (p.tags.isNotEmpty) ...[
                    const SizedBox(height: 16),
                    Wrap(spacing: 8, runSpacing: 8, children: [for (final tag in p.tags) Chip(label: Text(tag), visualDensity: VisualDensity.compact)]),
                  ],
                  if (onLike != null || onPass != null || onSuperLike != null) ...[
                    const SizedBox(height: 18),
                    Row(
                      mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                      crossAxisAlignment: CrossAxisAlignment.center,
                      children: [
                        if (onPass != null) _RoundAction(key: const Key('pass'), tooltip: 'Pass', icon: Icons.close, size: 60, onPressed: onPass!),
                        if (onSuperLike != null)
                          _RoundAction(
                              key: const Key('superlike'), tooltip: 'Super Like', icon: Icons.star_rounded, size: 52, onPressed: onSuperLike!, accent: t.colorScheme.tertiary),
                        if (onLike != null) _RoundAction(key: const Key('like'), tooltip: 'Like', icon: Icons.favorite, size: 72, onPressed: onLike!, filled: true),
                      ],
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// The top of the card: the photo when the server allows one, otherwise the
/// light of the person's mode with their avatar or veil.
class _Stage extends StatelessWidget {
  const _Stage({required this.profile, required this.light});
  final PublicProfile profile;
  final ModeLight light;

  @override
  Widget build(BuildContext context) {
    final p = profile;
    final hasPhotos = p.photos.isNotEmpty;
    // Short screens get a shorter stage so Like and Pass stay within reach.
    final compact = MediaQuery.sizeOf(context).height < 760;
    return SizedBox(
      height: hasPhotos ? (compact ? 320 : 420) : (compact ? 112 : 150),
      child: PhotoCarousel(
        profile: p,
        light: light,
        child: Stack(children: [
          Positioned(top: p.photos.length > 1 ? 26 : 14, left: 14, child: _ModeBadge(light: light)),
        ]),
      ),
    );
  }
}

/// Play button for a person's voice intro. The audio is fetched through the
/// authenticated API on first play, so visibility is re-checked by the server.
class _VoiceIntro extends ConsumerWidget {
  const _VoiceIntro({required this.profile});
  final PublicProfile profile;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = Theme.of(context);
    return Row(children: [
      Icon(Icons.graphic_eq, size: 18, color: t.colorScheme.onSurfaceVariant),
      const SizedBox(width: 6),
      Text('Voice intro', style: t.textTheme.bodySmall),
      const SizedBox(width: 8),
      VoicePlayback(
        seconds: profile.voiceIntroS,
        mime: 'audio/mp4',
        color: t.colorScheme.onSurface,
        load: () => ref.read(apiClientProvider).getBytes('/media/voice/${profile.ref}'),
      ),
    ]);
  }
}

/// "Watch video" appears when the server says this person has a reel we may
/// see (`reel_s` is never sent for anonymous profiles). The clip itself is
/// fetched on tap, so access is re-checked by the server.
class _ReelButton extends ConsumerWidget {
  const _ReelButton({required this.profile});
  final PublicProfile profile;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Padding(
      padding: const EdgeInsets.only(top: 12),
      child: Align(
        alignment: Alignment.centerLeft,
        child: OutlinedButton.icon(
          key: const Key('watch_profile_reel'),
          onPressed: () async {
            final clip = await ref.read(videosRepositoryProvider).reel(profile.ref);
            if (clip != null && context.mounted) await showClipDialog(context, ref, clip.id);
          },
          icon: const Icon(Icons.play_circle_outline),
          label: Text('Watch video (${profile.reelS}s)'),
        ),
      ),
    );
  }
}

/// "Read my story" appears when the server says this person has chapters we
/// may read (`chapters` is never sent for anonymous profiles).
class _ChaptersButton extends ConsumerWidget {
  const _ChaptersButton({required this.profile});
  final PublicProfile profile;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Padding(
      padding: const EdgeInsets.only(top: 12),
      child: Align(
        alignment: Alignment.centerLeft,
        child: OutlinedButton.icon(
          key: const Key('read_chapters'),
          onPressed: () => showChapters(context, ref, profile.ref),
          icon: const Icon(Icons.auto_stories_outlined),
          label: Text('Read my story (${profile.chapters})'),
        ),
      ),
    );
  }
}

class _ModeBadge extends StatelessWidget {
  const _ModeBadge({required this.light});
  final ModeLight light;
  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      decoration: BoxDecoration(color: Colors.black.withValues(alpha: 0.32), borderRadius: BorderRadius.circular(Radii.pill)),
      child: Text(light.label, style: Theme.of(context).textTheme.labelMedium?.copyWith(color: Colors.white)),
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact({required this.icon, required this.text});
  final IconData icon;
  final String text;
  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Row(mainAxisSize: MainAxisSize.min, children: [
      Icon(icon, size: 16, color: t.colorScheme.onSurfaceVariant),
      const SizedBox(width: 5),
      Text(text, style: t.textTheme.bodyMedium?.copyWith(color: t.colorScheme.onSurfaceVariant)),
    ]);
  }
}

/// Round action button: Like carries the Daylight gradient, the rest are quiet.
class _RoundAction extends StatelessWidget {
  const _RoundAction({super.key, required this.tooltip, required this.icon, required this.size, required this.onPressed, this.filled = false, this.accent});
  final String tooltip;
  final IconData icon;
  final double size;
  final VoidCallback onPressed;
  final bool filled;
  final Color? accent;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final fg = filled ? ModeLight.daylight.ink : (accent ?? t.colorScheme.onSurface);
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        gradient: filled ? ModeLight.daylight.gradient : null,
        color: filled ? null : t.colorScheme.surfaceContainerLow,
        border: filled ? null : Border.all(color: t.colorScheme.outlineVariant, width: 1.4),
        boxShadow: filled ? [BoxShadow(color: ModeLight.daylight.solid.withValues(alpha: 0.4), blurRadius: 20, offset: const Offset(0, 8))] : null,
      ),
      child: IconButton(
        tooltip: tooltip,
        onPressed: onPressed,
        iconSize: size * 0.42,
        color: fg,
        icon: Icon(icon),
        style: IconButton.styleFrom(minimumSize: Size.square(size), fixedSize: Size.square(size)),
      ),
    );
  }
}
