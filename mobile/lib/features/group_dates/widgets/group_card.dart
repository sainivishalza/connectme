import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';

import '../../../core/models.dart';
import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/avatar.dart';

/// Summary of a group: name, activity, size and members as visible to the viewer.
class GroupCard extends StatelessWidget {
  const GroupCard({super.key, required this.group, this.onTap, this.trailing});
  final Group group;
  final VoidCallback? onTap;
  final Widget? trailing;

  static IconData iconFor(String activity) => switch (activity) {
        'dinner' => Icons.restaurant,
        'hiking' => Icons.terrain,
        'concert' => Icons.music_note,
        'bar_crawl' => Icons.local_bar,
        _ => Icons.celebration,
      };

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final g = group;
    return Card(
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(Radii.card),
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    width: 48,
                    height: 48,
                    decoration: BoxDecoration(
                      color: t.colorScheme.secondaryContainer,
                      borderRadius: BorderRadius.circular(Radii.control),
                    ),
                    child: Icon(iconFor(g.activity), color: t.colorScheme.onSecondaryContainer),
                  ),
                  const SizedBox(width: 14),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(g.name, style: t.textTheme.titleMedium),
                        const SizedBox(height: 4),
                        Wrap(spacing: 12, runSpacing: 2, children: [
                          _Fact(g.type == 'double' ? 'Double date' : 'Group date'),
                          _Fact(g.activityName),
                          _Fact('${g.memberCount} of ${g.targetSize}'),
                          if (g.distance.isNotEmpty) _Fact(g.distance),
                        ]),
                      ],
                    ),
                  ),
                  ?trailing,
                ],
              ),
              if (g.description.isNotEmpty) ...[const SizedBox(height: 8), Text(g.description)],
              if (g.members.isNotEmpty) ...[
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final m in g.members)
                      Chip(
                        avatar: Avatar(name: m.displayName, anonymous: m.anonymous, radius: 12),
                        label: Text(m.title),
                        visualDensity: VisualDensity.compact,
                      ),
                  ],
                ),
              ],
              if (!g.ready) ...[
                const SizedBox(height: 8),
                Text('Waiting for ${g.targetSize - g.memberCount} more', style: t.textTheme.bodySmall?.copyWith(color: t.colorScheme.primary)),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact(this.text);
  final String text;
  @override
  Widget build(BuildContext context) =>
      Text(text, style: Theme.of(context).textTheme.bodySmall);
}

/// Activity chips used by the create form and browse filter.
class ActivityPicker extends StatelessWidget {
  const ActivityPicker({super.key, required this.value, required this.onChanged, this.allowNone = false});
  final String? value;
  final ValueChanged<String?> onChanged;
  final bool allowNone;

  static const activities = ['dinner', 'hiking', 'concert', 'bar_crawl', 'custom'];

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 8,
      children: [
        if (allowNone)
          ChoiceChip(label: Text(AppLocalizations.of(context)?.sAny ?? 'Any'), selected: value == null, onSelected: (_) => onChanged(null)),
        for (final a in activities)
          ChoiceChip(
            key: Key('activity_$a'),
            avatar: Icon(GroupCard.iconFor(a), size: 16),
            label: Text(a.replaceAll('_', ' ')),
            selected: value == a,
            onSelected: (_) => onChanged(a),
          ),
      ],
    );
  }
}
