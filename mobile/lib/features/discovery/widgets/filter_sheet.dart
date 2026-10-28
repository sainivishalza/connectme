import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_button.dart';
import '../discovery_repository.dart';

class FilterSheet extends ConsumerStatefulWidget {
  const FilterSheet({super.key});
  @override
  ConsumerState<FilterSheet> createState() => _FilterSheetState();
}

class _FilterSheetState extends ConsumerState<FilterSheet> {
  late NearbyFilter _f = ref.read(nearbyFilterProvider);

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(16, 0, 16, MediaQuery.of(context).viewInsets.bottom + 16),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Distance', style: Theme.of(context).textTheme.titleMedium),
          Wrap(
            spacing: 8,
            children: [
              for (final r in NearbyFilter.radii)
                ChoiceChip(
                  key: Key('radius_$r'),
                  label: Text('$r km'),
                  selected: _f.radiusKm == r,
                  onSelected: (_) => setState(() => _f = _f.copyWith(radiusKm: r)),
                ),
            ],
          ),
          const SizedBox(height: 16),
          Text('Age ${_f.ageMin}–${_f.ageMax}', style: Theme.of(context).textTheme.titleMedium),
          RangeSlider(
            values: RangeValues(_f.ageMin.toDouble(), _f.ageMax.toDouble()),
            min: 18,
            max: 80,
            divisions: 62,
            labels: RangeLabels('${_f.ageMin}', '${_f.ageMax}'),
            onChanged: (v) => setState(() => _f = _f.copyWith(ageMin: v.start.round(), ageMax: v.end.round())),
          ),
          const SizedBox(height: 8),
          Text('What they want', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 4),
          _Options(label: 'Looking for', keyPrefix: 'f_goal', options: DetailLabels.goal, value: _f.goal, onChanged: (v) => setState(() => _f = _f.copyWith(goal: v))),
          _Options(label: 'Kids', keyPrefix: 'f_kids', options: DetailLabels.kids, value: _f.kids, onChanged: (v) => setState(() => _f = _f.copyWith(kids: v))),
          _Options(label: 'Smoking', keyPrefix: 'f_smoking', options: DetailLabels.frequency, value: _f.smoking, onChanged: (v) => setState(() => _f = _f.copyWith(smoking: v))),
          _Options(label: 'Drinking', keyPrefix: 'f_drinking', options: DetailLabels.frequency, value: _f.drinking, onChanged: (v) => setState(() => _f = _f.copyWith(drinking: v))),
          SwitchListTile(
            key: const Key('f_verified'),
            contentPadding: EdgeInsets.zero,
            title: const Text('ID verified only'),
            value: _f.verifiedOnly,
            onChanged: (v) => setState(() => _f = _f.copyWith(verifiedOnly: v)),
          ),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
              key: const Key('clear_filters'),
              onPressed: () => setState(() => _f = NearbyFilter(radiusKm: _f.radiusKm, ageMin: _f.ageMin, ageMax: _f.ageMax, tags: _f.tags, gender: _f.gender)),
              child: const Text('Clear what they want'),
            ),
          ),
          AppButton(
            key: const Key('apply_filters'),
            label: 'Apply',
            onPressed: () {
              ref.read(nearbyFilterProvider.notifier).set(_f);
              Navigator.pop(context);
            },
          ),
        ],
      ),
    );
  }
}

/// One optional filter: tap a choice to turn it on, tap it again to turn it off.
class _Options extends StatelessWidget {
  const _Options({required this.label, required this.keyPrefix, required this.options, required this.value, required this.onChanged});
  final String label;
  final String keyPrefix;
  final Map<String, String> options;
  final String? value;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text(label, style: Theme.of(context).textTheme.labelLarge),
        const SizedBox(height: 4),
        Wrap(spacing: 8, runSpacing: 4, children: [
          for (final e in options.entries)
            ChoiceChip(
              key: Key('${keyPrefix}_${e.key}'),
              label: Text(e.value),
              selected: value == e.key,
              onSelected: (on) => onChanged(on ? e.key : ''),
            ),
        ]),
      ]),
    );
  }
}
