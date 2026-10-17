import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

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
    return Padding(
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
