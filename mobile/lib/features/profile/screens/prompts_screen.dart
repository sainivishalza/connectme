import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../profile_repository.dart';

final promptBankProvider = FutureProvider.autoDispose<List<PromptDef>>((ref) => ref.watch(profileRepositoryProvider).promptBank());

/// Answer up to three prompts. They appear on your profile once it is visible,
/// as an easy way for people to start a conversation.
class PromptsScreen extends ConsumerWidget {
  const PromptsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final me = ref.watch(meProvider);
    final bank = ref.watch(promptBankProvider);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.profilePrompts ?? 'Your prompts')),
      body: switch ((me, bank)) {
        (AsyncData(value: final p), AsyncData(value: final b)) => _Editor(initial: p.prompts, bank: b),
        (AsyncError(error: final e), _) || (_, AsyncError(error: final e)) => ErrorView(error: e, onRetry: () {
            ref.invalidate(meProvider);
            ref.invalidate(promptBankProvider);
          }),
        _ => const Loader(),
      },
    );
  }
}

class _Editor extends ConsumerStatefulWidget {
  const _Editor({required this.initial, required this.bank});
  final List<PromptAnswer> initial;
  final List<PromptDef> bank;

  @override
  ConsumerState<_Editor> createState() => _EditorState();
}

class _EditorState extends ConsumerState<_Editor> {
  late final List<String?> _keys = List.generate(3, (i) => i < widget.initial.length ? widget.initial[i].key : null);
  late final List<TextEditingController> _answers =
      List.generate(3, (i) => TextEditingController(text: i < widget.initial.length ? widget.initial[i].answer : ''));
  bool _busy = false;

  @override
  void dispose() {
    for (final c in _answers) {
      c.dispose();
    }
    super.dispose();
  }

  String _text(String? key) => widget.bank.where((p) => p.key == key).map((p) => p.text).firstOrNull ?? 'Choose a prompt';

  Future<void> _choose(int slot) async {
    final taken = {for (var i = 0; i < 3; i++) if (i != slot && _keys[i] != null) _keys[i]!};
    final picked = await showModalBottomSheet<String>(
      context: context, useRootNavigator: true,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (c) => SafeArea(
        child: ListView(shrinkWrap: true, children: [
          for (final p in widget.bank)
            ListTile(
              key: Key('choose_${p.key}'),
              title: Text(p.text),
              enabled: !taken.contains(p.key),
              trailing: _keys[slot] == p.key ? const Icon(Icons.check) : null,
              onTap: () => Navigator.pop(c, p.key),
            ),
        ]),
      ),
    );
    if (picked != null) setState(() => _keys[slot] = picked);
  }

  Future<void> _save() async {
    final chosen = <({String key, String answer})>[
      for (var i = 0; i < 3; i++)
        if (_keys[i] != null && _answers[i].text.trim().isNotEmpty) (key: _keys[i]!, answer: _answers[i].text.trim()),
    ];
    setState(() => _busy = true);
    final ok = await guarded(context, () async {
      await ref.read(profileRepositoryProvider).savePrompts(chosen);
      ref.invalidate(meProvider);
    });
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      showMessage(context, 'Saved');
      context.pop();
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return ListView(padding: const EdgeInsets.all(16), children: [
      Text('Pick up to three questions and answer in a line or two. Keep it light: no phone numbers, links or social handles.', style: t.textTheme.bodyMedium),
      const SizedBox(height: 16),
      for (var i = 0; i < 3; i++) ...[
        Card(
          child: Padding(
            padding: const EdgeInsets.all(14),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              InkWell(
                key: Key('slot_$i'),
                onTap: () => _choose(i),
                child: Row(children: [
                  Expanded(child: Text(_text(_keys[i]), style: t.textTheme.titleSmall)),
                  const Icon(Icons.expand_more),
                ]),
              ),
              if (_keys[i] != null)
                TextField(
                  key: Key('answer_$i'),
                  controller: _answers[i],
                  maxLength: 200,
                  maxLines: 2,
                  decoration: const InputDecoration(hintText: 'Your answer'),
                ),
            ]),
          ),
        ),
        const SizedBox(height: 10),
      ],
      FilledButton(key: const Key('save_prompts'), onPressed: _busy ? null : _save, child: Text(_busy ? 'Saving…' : 'Save')),
    ]);
  }
}
