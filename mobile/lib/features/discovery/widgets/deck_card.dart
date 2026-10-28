import '../../profile/widgets/detail_widgets.dart';
import 'package:flutter/material.dart';

import '../../../core/models.dart';
import '../../../core/theme/tokens.dart';
import 'photo_carousel.dart';

/// One person in the swipe deck: the photo fills the card and the name, a few
/// facts and a line about them sit on a dark fade at the bottom. Tap the
/// middle to open the whole profile; the edges flip through photos.
/// What a screen reader says for a card: who, how old, how well they match,
/// and the facts. An anonymous person is just "Anonymous".
String describeProfile(PublicProfile p) => [
      p.age == null ? p.title : '${p.title}, ${p.age}',
      if (p.idVerified) 'ID verified',
      if (p.quizMatch > 0) '${p.quizMatch}% quiz match',
      if (p.charmLevel >= 2) 'Charm level ${p.charmLevel}',
      if (p.distance.isNotEmpty) p.distance,
      if (p.intro.isNotEmpty) p.intro,
    ].join('. ');

class DeckCard extends StatelessWidget {
  const DeckCard({super.key, required this.profile, this.like = 0, this.nope = 0, this.superLike = 0, this.onOpen});

  final PublicProfile profile;

  /// 0..1 strength of the drag towards each action; fades the stamps in.
  final double like;
  final double nope;
  final double superLike;
  final VoidCallback? onOpen;

  @override
  Widget build(BuildContext context) {
    final p = profile;
    final t = Theme.of(context);
    final light = ModeLight.of(p.anonymous && p.mode == 'open' ? 'incognito' : p.mode);
    const white = Colors.white;
    final facts = <String>[
      if (p.distance.isNotEmpty) p.distance,
      if (p.hotelName.isNotEmpty) p.hotelName,
      if (p.details?.goal.isNotEmpty ?? false) DetailLabels.goal[p.details!.goal] ?? '',
    ].where((s) => s.isNotEmpty).toList();
    final line = p.intro.isNotEmpty ? p.intro : (p.prompts.isNotEmpty ? p.prompts.first.answer : '');
    return ClipRRect(
      borderRadius: BorderRadius.circular(Radii.hero),
      child: DecoratedBox(
        decoration: BoxDecoration(color: t.colorScheme.surfaceContainerHighest),
        child: PhotoCarousel(
          profile: p,
          light: light,
          onCenterTap: onOpen,
          child: Stack(fit: StackFit.expand, children: [
            // Fade for legible text over any photo.
            const IgnorePointer(
              child: DecoratedBox(
                decoration: BoxDecoration(
                  gradient: LinearGradient(begin: Alignment.center, end: Alignment.bottomCenter, colors: [Color(0x00000000), Color(0xCC000000)]),
                ),
              ),
            ),
            Positioned(
              left: 20,
              right: 20,
              bottom: 18,
              child: IgnorePointer(
                child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Row(children: [
                    Flexible(
                      child: Text(
                        p.age == null ? p.title : '${p.title}, ${p.age}',
                        key: const Key('deck_name'),
                        style: t.textTheme.headlineMedium?.copyWith(color: white, fontWeight: FontWeight.w700),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    if (p.idVerified) ...[const SizedBox(width: 8), const Icon(Icons.verified, color: white, size: 24, key: Key('verified_badge'))],
                    if (p.charmLevel >= 2) ...[const SizedBox(width: 8), CharmBadge(level: p.charmLevel, onDark: true)],
                    if (p.vip) ...[const SizedBox(width: 6), Icon(Icons.workspace_premium, color: t.colorScheme.tertiary, size: 24)],
                  ]),
                  if (p.quizMatch > 0) ...[const SizedBox(height: 8), QuizMatchBadge(percent: p.quizMatch, onDark: true)],
                  if (facts.isNotEmpty) ...[
                    const SizedBox(height: 8),
                    Wrap(spacing: 8, runSpacing: 6, children: [
                      for (final f in facts)
                        DecoratedBox(
                          decoration: BoxDecoration(color: Colors.white.withValues(alpha: 0.2), borderRadius: BorderRadius.circular(20)),
                          child: Padding(padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4), child: Text(f, style: t.textTheme.labelMedium?.copyWith(color: white))),
                        ),
                    ]),
                  ],
                  if (line.isNotEmpty) ...[
                    const SizedBox(height: 10),
                    Text(line, maxLines: 2, overflow: TextOverflow.ellipsis, style: t.textTheme.bodyLarge?.copyWith(color: white.withValues(alpha: 0.95))),
                  ],
                  if (p.anonymous) ...[
                    const SizedBox(height: 8),
                    Row(children: [
                      const Icon(Icons.lock_outline, size: 16, color: white),
                      const SizedBox(width: 6),
                      Text('Anonymous until you both reveal', style: t.textTheme.labelMedium?.copyWith(color: white)),
                    ]),
                  ],
                  const SizedBox(height: 4),
                  Align(
                    alignment: Alignment.centerRight,
                    child: Text('Tap for more', style: t.textTheme.labelSmall?.copyWith(color: white.withValues(alpha: 0.7))),
                  ),
                ]),
              ),
            ),
            Positioned(top: 34, left: 18, child: _Stamp(key: const Key('stamp_like'), text: 'LIKE', color: const Color(0xFF2EC27E), amount: like, angle: -0.35)),
            Positioned(top: 34, right: 18, child: _Stamp(key: const Key('stamp_nope'), text: 'NOPE', color: const Color(0xFFE5484D), amount: nope, angle: 0.35)),
            Positioned(
              left: 0,
              right: 0,
              top: 130,
              child: Center(child: _Stamp(key: const Key('stamp_super'), text: 'SUPER LIKE', color: const Color(0xFF3E8BFF), amount: superLike, angle: 0)),
            ),
          ]),
        ),
      ),
    );
  }
}

class _Stamp extends StatelessWidget {
  const _Stamp({super.key, required this.text, required this.color, required this.amount, required this.angle});
  final String text;
  final Color color;
  final double amount;
  final double angle;

  @override
  Widget build(BuildContext context) {
    return IgnorePointer(
      child: Opacity(
        opacity: amount.clamp(0, 1),
        child: Transform.rotate(
          angle: angle,
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
            decoration: BoxDecoration(border: Border.all(color: color, width: 4), borderRadius: BorderRadius.circular(8)),
            child: Text(text, style: Theme.of(context).textTheme.headlineSmall?.copyWith(color: color, fontWeight: FontWeight.w900, letterSpacing: 2)),
          ),
        ),
      ),
    );
  }
}
