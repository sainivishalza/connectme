import 'package:flutter/material.dart';

import '../../../core/models.dart';

IconData detailIcon(DetailIcon i) => switch (i) {
      DetailIcon.goal => Icons.favorite_border,
      DetailIcon.height => Icons.height,
      DetailIcon.job => Icons.work_outline,
      DetailIcon.education => Icons.school_outlined,
      DetailIcon.hometown => Icons.home_outlined,
      DetailIcon.kids => Icons.child_care_outlined,
      DetailIcon.drinking => Icons.local_bar_outlined,
      DetailIcon.smoking => Icons.smoking_rooms_outlined,
      DetailIcon.exercise => Icons.fitness_center_outlined,
      DetailIcon.pets => Icons.pets_outlined,
      DetailIcon.languages => Icons.translate,
      DetailIcon.zodiac => Icons.auto_awesome_outlined,
    };

/// The facts about a person as small labelled chips.
class DetailChips extends StatelessWidget {
  const DetailChips({super.key, required this.facts});
  final List<DetailFact> facts;

  @override
  Widget build(BuildContext context) {
    if (facts.isEmpty) return const SizedBox.shrink();
    final t = Theme.of(context);
    return Wrap(spacing: 8, runSpacing: 8, children: [
      for (final f in facts)
        DecoratedBox(
          decoration: BoxDecoration(
            color: t.colorScheme.surfaceContainerHighest.withValues(alpha: 0.6),
            borderRadius: BorderRadius.circular(20),
          ),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
            child: Row(mainAxisSize: MainAxisSize.min, children: [
              Icon(detailIcon(f.icon), size: 16, color: t.colorScheme.onSurfaceVariant),
              const SizedBox(width: 6),
              Flexible(child: Text(f.text, style: t.textTheme.labelMedium, overflow: TextOverflow.ellipsis)),
            ]),
          ),
        ),
    ]);
  }
}

/// A prompt and its answer, set apart so it reads like a conversation starter.
class PromptBlock extends StatelessWidget {
  const PromptBlock({super.key, required this.prompt});
  final PromptAnswer prompt;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: t.colorScheme.primaryContainer.withValues(alpha: 0.35),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text(prompt.prompt, style: t.textTheme.labelMedium?.copyWith(color: t.colorScheme.onSurfaceVariant)),
        const SizedBox(height: 6),
        Text(prompt.answer, style: t.textTheme.titleMedium),
      ]),
    );
  }
}

/// "Lv 7" pill for a member's Charm level (level 2 and up, shown by choice).
class CharmBadge extends StatelessWidget {
  const CharmBadge({super.key, required this.level, this.onDark = false});
  final int level;
  final bool onDark;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final fg = onDark ? Colors.white : t.colorScheme.onTertiaryContainer;
    return Tooltip(
      message: 'Charm level $level',
      child: DecoratedBox(
        key: const Key('charm_badge'),
        decoration: BoxDecoration(
          color: onDark ? Colors.white.withValues(alpha: 0.22) : t.colorScheme.tertiaryContainer,
          borderRadius: BorderRadius.circular(20),
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
          child: Row(mainAxisSize: MainAxisSize.min, children: [
            Icon(Icons.auto_awesome, size: 14, color: fg),
            const SizedBox(width: 4),
            Text('Lv $level', style: t.textTheme.labelMedium?.copyWith(color: fg, fontWeight: FontWeight.w700)),
          ]),
        ),
      ),
    );
  }
}

/// "80% quiz match": how closely the viewer's quiz answers line up with this person's.
class QuizMatchBadge extends StatelessWidget {
  const QuizMatchBadge({super.key, required this.percent, this.onDark = false});
  final int percent;
  final bool onDark;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final fg = onDark ? Colors.white : t.colorScheme.onPrimaryContainer;
    return DecoratedBox(
      key: const Key('quiz_badge'),
      decoration: BoxDecoration(
        color: onDark ? Colors.white.withValues(alpha: 0.22) : t.colorScheme.primaryContainer,
        borderRadius: BorderRadius.circular(20),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
        child: Row(mainAxisSize: MainAxisSize.min, children: [
          Icon(Icons.psychology_alt_outlined, size: 14, color: fg),
          const SizedBox(width: 4),
          Text('$percent% quiz match', style: t.textTheme.labelMedium?.copyWith(color: fg, fontWeight: FontWeight.w700)),
        ]),
      ),
    );
  }
}

/// The blue-tick style badge for a member whose ID a moderator checked.
class VerifiedBadge extends StatelessWidget {
  const VerifiedBadge({super.key, this.size = 22});
  final double size;

  @override
  Widget build(BuildContext context) =>
      Tooltip(message: 'ID verified', child: Icon(Icons.verified, key: const Key('verified_badge'), size: size, color: Theme.of(context).colorScheme.primary));
}
