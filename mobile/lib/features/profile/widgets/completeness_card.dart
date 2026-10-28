import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';

/// Where each next step is done in the app.
String stepRoute(String key) => switch (key) {
      'set_goal' || 'fill_lifestyle' => '/profile/about',
      'answer_prompts' => '/profile/prompts',
      'verify_id' => '/profile/verify-id',
      _ => '/profile/edit',
    };

/// A progress ring and the one thing to do next. Hidden once the profile is
/// complete, so it never nags.
class CompletenessCard extends StatelessWidget {
  const CompletenessCard({super.key, required this.completeness});
  final Completeness completeness;

  @override
  Widget build(BuildContext context) {
    final c = completeness;
    if (c.nextSteps.isEmpty) return const SizedBox.shrink();
    final t = Theme.of(context);
    final next = c.nextSteps.first;
    return Card(
      key: const Key('completeness'),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: () => context.push(stepRoute(next)),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(children: [
            SizedBox(
              width: 56,
              height: 56,
              child: Stack(alignment: Alignment.center, children: [
                CircularProgressIndicator(value: c.percent / 100, strokeWidth: 6, backgroundColor: t.colorScheme.surfaceContainerHighest),
                Text('${c.percent}%', key: const Key('completeness_percent'), style: t.textTheme.labelLarge),
              ]),
            ),
            const SizedBox(width: 16),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text('Complete your profile', style: t.textTheme.titleSmall),
                const SizedBox(height: 2),
                Text(Completeness.stepText(next), key: const Key('completeness_next'), style: t.textTheme.bodyMedium),
                if (c.nextSteps.length > 1) Text('${c.nextSteps.length - 1} more after that', style: t.textTheme.bodySmall?.copyWith(color: t.colorScheme.onSurfaceVariant)),
              ]),
            ),
            const Icon(Icons.chevron_right),
          ]),
        ),
      ),
    );
  }
}
