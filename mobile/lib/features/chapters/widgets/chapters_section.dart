import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../chapters_repository.dart';

/// Tell your story in up to five short chapters. Only people who can see your
/// full profile can read them; they stay hidden while you are anonymous.
class ChaptersSection extends ConsumerStatefulWidget {
  const ChaptersSection({super.key});

  @override
  ConsumerState<ChaptersSection> createState() => _ChaptersSectionState();
}

class _Row {
  _Row([String title = '', String body = ''])
      : title = TextEditingController(text: title),
        body = TextEditingController(text: body);
  final TextEditingController title;
  final TextEditingController body;
  void dispose() {
    title.dispose();
    body.dispose();
  }
}

class _ChaptersSectionState extends ConsumerState<ChaptersSection> {
  final List<_Row> _rows = [];
  bool _loaded = false;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    for (final r in _rows) {
      r.dispose();
    }
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final cs = await ref.read(chaptersRepositoryProvider).mine();
      if (!mounted) return;
      setState(() {
        _rows.addAll([for (final c in cs) _Row(c.title, c.body)]);
        _loaded = true;
      });
    } catch (_) {
      if (mounted) setState(() => _loaded = true);
    }
  }

  Future<void> _save() async {
    setState(() => _saving = true);
    final ok = await guarded(context, () async {
      await ref.read(chaptersRepositoryProvider).save([for (final r in _rows) Chapter(title: r.title.text.trim(), body: r.body.text.trim())]);
    });
    if (mounted) {
      setState(() => _saving = false);
      if (ok) showMessage(context, 'Chapters saved');
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    if (!_loaded) return const SizedBox.shrink();
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text('Your story', style: t.textTheme.titleMedium),
      const SizedBox(height: 4),
      Text('Up to five chapters, like "The year I moved abroad". Only people who can see your full profile can read them.', style: t.textTheme.bodySmall),
      const SizedBox(height: 10),
      for (var i = 0; i < _rows.length; i++)
        Card(
          key: Key('chapter_$i'),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
            child: Column(children: [
              Row(children: [
                Expanded(child: TextField(key: Key('chapter_title_$i'), controller: _rows[i].title, maxLength: 60, decoration: InputDecoration(labelText: 'Chapter ${i + 1} title'))),
                IconButton(
                  key: Key('chapter_remove_$i'),
                  tooltip: 'Remove chapter',
                  icon: const Icon(Icons.close),
                  onPressed: () => setState(() => _rows.removeAt(i).dispose()),
                ),
              ]),
              TextField(key: Key('chapter_body_$i'), controller: _rows[i].body, maxLength: 600, minLines: 2, maxLines: 5, decoration: const InputDecoration(labelText: 'What happened?')),
            ]),
          ),
        ),
      Wrap(spacing: 8, children: [
        if (_rows.length < 5) OutlinedButton.icon(key: const Key('add_chapter'), onPressed: () => setState(() => _rows.add(_Row())), icon: const Icon(Icons.add), label: const Text('Add a chapter')),
        FilledButton.tonal(key: const Key('save_chapters'), onPressed: _saving ? null : _save, child: Text(_saving ? 'Saving' : 'Save chapters')),
      ]),
    ]);
  }
}

/// Opens someone's chapters in a sheet. The server answers "nothing" for
/// profiles you may not see, so this shows an empty note rather than an error.
Future<void> showChapters(BuildContext context, WidgetRef ref, String profileRef) async {
  final chapters = await ref.read(chaptersRepositoryProvider).of(profileRef);
  if (!context.mounted) return;
  await showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (c) {
      final t = Theme.of(c);
      return SafeArea(
        child: ConstrainedBox(
          constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(c).height * 0.8),
          child: ListView(padding: const EdgeInsets.fromLTRB(20, 0, 20, 24), shrinkWrap: true, children: [
            if (chapters.isEmpty) Text('Nothing to read yet.', style: t.textTheme.bodyMedium),
            for (var i = 0; i < chapters.length; i++) ...[
              Text(chapters[i].title, style: t.textTheme.titleLarge),
              const SizedBox(height: 6),
              Text(chapters[i].body, style: t.textTheme.bodyLarge),
              const SizedBox(height: 20),
            ],
          ]),
        ),
      );
    },
  );
}
