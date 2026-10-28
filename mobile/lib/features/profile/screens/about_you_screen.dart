import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../profile_repository.dart';

/// "About you": what you are looking for, how you live, and a few facts.
/// What others see depends on your mode: the first group is shown on every
/// card, even anonymous ones; the second only when your profile is visible.
class AboutYouScreen extends ConsumerWidget {
  const AboutYouScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final me = ref.watch(meProvider);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.profileAboutYou ?? 'About you')),
      body: me.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(meProvider)),
        data: (p) => _Form(initial: p.details),
      ),
    );
  }
}

class _Form extends ConsumerStatefulWidget {
  const _Form({required this.initial});
  final ProfileDetails initial;

  @override
  ConsumerState<_Form> createState() => _FormState();
}

class _FormState extends ConsumerState<_Form> {
  late String _goal = widget.initial.goal;
  late String _kids = widget.initial.kids;
  late String _drinking = widget.initial.drinking;
  late String _smoking = widget.initial.smoking;
  late String _exercise = widget.initial.exercise;
  late String _pets = widget.initial.pets;
  late String _education = widget.initial.education;
  late String _zodiac = widget.initial.zodiac;
  late List<String> _languages = [...widget.initial.languages];
  late int _height = widget.initial.heightCm;
  late final _job = TextEditingController(text: widget.initial.jobTitle);
  late final _hometown = TextEditingController(text: widget.initial.hometown);
  bool _busy = false;

  @override
  void dispose() {
    _job.dispose();
    _hometown.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    setState(() => _busy = true);
    final ok = await guarded(context, () async {
      await ref.read(profileRepositoryProvider).saveDetails({
        'goal': _goal,
        'kids': _kids,
        'drinking': _drinking,
        'smoking': _smoking,
        'exercise': _exercise,
        'pets': _pets,
        'education': _education,
        'zodiac': _zodiac,
        'languages': _languages,
        'height_cm': _height,
        'job_title': _job.text.trim(),
        'hometown': _hometown.text.trim(),
      });
      ref.invalidate(meProvider);
    });
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      showMessage(context, 'Saved');
      context.pop();
    }
  }

  Future<void> _pickLanguages() async {
    final chosen = await showModalBottomSheet<List<String>>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (c) => _LanguageSheet(selected: _languages),
    );
    if (chosen != null) setState(() => _languages = chosen);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return ListView(padding: const EdgeInsets.fromLTRB(16, 8, 16, 32), children: [
      _Section(title: 'Shown on every card', note: 'These say what you want, not who you are, so they stay visible even in Incognito and Hotel mode.'),
      _Choice(label: 'Looking for', options: DetailLabels.goal, value: _goal, keyPrefix: 'goal', onChanged: (v) => setState(() => _goal = v)),
      _Choice(label: 'Kids', options: DetailLabels.kids, value: _kids, keyPrefix: 'kids', onChanged: (v) => setState(() => _kids = v)),
      _Choice(label: 'Drinking', options: DetailLabels.frequency, value: _drinking, keyPrefix: 'drinking', onChanged: (v) => setState(() => _drinking = v)),
      _Choice(label: 'Smoking', options: DetailLabels.frequency, value: _smoking, keyPrefix: 'smoking', onChanged: (v) => setState(() => _smoking = v)),
      _Choice(label: 'Exercise', options: DetailLabels.exercise, value: _exercise, keyPrefix: 'exercise', onChanged: (v) => setState(() => _exercise = v)),
      _Choice(label: 'Pets', options: DetailLabels.pets, value: _pets, keyPrefix: 'pets', onChanged: (v) => setState(() => _pets = v)),
      const SizedBox(height: 4),
      Text('Languages', style: t.textTheme.titleSmall),
      const SizedBox(height: 6),
      Wrap(spacing: 8, runSpacing: 8, children: [
        for (final l in _languages) Chip(label: Text(DetailLabels.languages[l] ?? l)),
        ActionChip(key: const Key('pick_languages'), avatar: const Icon(Icons.add, size: 18), label: Text(_languages.isEmpty ? 'Add languages' : 'Change'), onPressed: _pickLanguages),
      ]),
      const SizedBox(height: 24),
      _Section(title: 'Shown only when your profile is visible', note: 'Hidden while you are in Incognito or Hotel mode, until you both reveal.'),
      Text(_height == 0 ? 'Height: not shown' : 'Height: $_height cm', style: t.textTheme.titleSmall),
      Row(children: [
        Expanded(
          child: Slider(
            key: const Key('height'),
            min: 140,
            max: 210,
            divisions: 70,
            value: (_height == 0 ? 170 : _height).toDouble().clamp(140, 210),
            label: _height == 0 ? '' : '$_height cm',
            onChanged: (v) => setState(() => _height = v.round()),
          ),
        ),
        if (_height != 0) TextButton(onPressed: () => setState(() => _height = 0), child: Text(AppLocalizations.of(context)?.sClear ?? 'Clear')),
      ]),
      TextField(key: const Key('job'), controller: _job, maxLength: 60, decoration: const InputDecoration(labelText: 'Job title', hintText: 'Nurse, designer, student…')),
      TextField(key: const Key('hometown'), controller: _hometown, maxLength: 60, decoration: const InputDecoration(labelText: 'Hometown')),
      _Choice(label: 'Education', options: DetailLabels.education, value: _education, keyPrefix: 'edu', onChanged: (v) => setState(() => _education = v)),
      _Choice(label: 'Star sign', options: DetailLabels.zodiac, value: _zodiac, keyPrefix: 'zodiac', onChanged: (v) => setState(() => _zodiac = v)),
      const SizedBox(height: 20),
      FilledButton(key: const Key('save_about'), onPressed: _busy ? null : _save, child: Text(_busy ? 'Saving…' : 'Save')),
    ]);
  }
}

class _Section extends StatelessWidget {
  const _Section({required this.title, required this.note});
  final String title;
  final String note;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text(title, style: t.textTheme.titleMedium),
        const SizedBox(height: 2),
        Text(note, style: t.textTheme.bodySmall?.copyWith(color: t.colorScheme.onSurfaceVariant)),
      ]),
    );
  }
}

/// Single choice from a set; tapping the chosen one again clears it.
class _Choice extends StatelessWidget {
  const _Choice({required this.label, required this.options, required this.value, required this.keyPrefix, required this.onChanged});
  final String label;
  final Map<String, String> options;
  final String value;
  final String keyPrefix;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text(label, style: t.textTheme.titleSmall),
        const SizedBox(height: 6),
        Wrap(spacing: 8, runSpacing: 8, children: [
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

class _LanguageSheet extends StatefulWidget {
  const _LanguageSheet({required this.selected});
  final List<String> selected;

  @override
  State<_LanguageSheet> createState() => _LanguageSheetState();
}

class _LanguageSheetState extends State<_LanguageSheet> {
  late final Set<String> _on = {...widget.selected};

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Column(mainAxisSize: MainAxisSize.min, children: [
        Text('Languages you speak (up to 5)', style: Theme.of(context).textTheme.titleMedium),
        Flexible(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(16),
            child: Wrap(spacing: 8, runSpacing: 8, children: [
              for (final e in DetailLabels.languages.entries)
                FilterChip(
                  key: Key('lang_${e.key}'),
                  label: Text(e.value),
                  selected: _on.contains(e.key),
                  onSelected: (on) => setState(() {
                    if (on && _on.length < 5) _on.add(e.key);
                    if (!on) _on.remove(e.key);
                  }),
                ),
            ]),
          ),
        ),
        Padding(
          padding: const EdgeInsets.all(16),
          child: SizedBox(width: double.infinity, child: FilledButton(onPressed: () => Navigator.pop(context, _on.toList()), child: Text(AppLocalizations.of(context)?.sDone ?? 'Done'))),
        ),
      ]),
    );
  }
}
