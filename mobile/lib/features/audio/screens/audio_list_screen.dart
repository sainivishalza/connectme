import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../audio_repository.dart';

/// Live audio rooms, with a button to open your own.
class AudioListScreen extends ConsumerWidget {
  const AudioListScreen({super.key});

  Future<void> _start(BuildContext context, WidgetRef ref) async {
    final repo = ref.read(audioRepositoryProvider);
    final started = await showModalBottomSheet<StartedAudio>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _StartSheet(repo: repo),
    );
    if (started != null && context.mounted) {
      await context.push('/audio/${started.room.id}', extra: started);
      ref.invalidate(audioRoomsProvider);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final rooms = ref.watch(audioRoomsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Audio rooms')),
      floatingActionButton: FloatingActionButton.extended(
        key: const Key('start_audio'),
        onPressed: () => _start(context, ref),
        icon: const Icon(Icons.mic),
        label: const Text('Start a room'),
      ),
      body: AsyncListView<AudioRoom>(
        value: rooms,
        onRefresh: () async => ref.refresh(audioRoomsProvider.future),
        emptyTitle: 'No rooms open',
        emptyMessage: 'Start one and invite the conversation.',
        emptyIcon: Icons.graphic_eq,
        itemBuilder: (c, r) => Card(
          child: ListTile(
            key: Key('audio_${r.id}'),
            leading: CircleAvatar(child: Icon(r.anonymous ? Icons.masks_outlined : Icons.graphic_eq)),
            title: Text(r.title, maxLines: 1, overflow: TextOverflow.ellipsis),
            subtitle: Text('${r.speakers} on stage · ${r.listeners} listening${r.anonymous ? ' · anonymous' : ''}'),
            trailing: const Icon(Icons.chevron_right),
            onTap: () async {
              await c.push('/audio/${r.id}');
              ref.invalidate(audioRoomsProvider);
            },
          ),
        ),
      ),
    );
  }
}

class _StartSheet extends StatefulWidget {
  const _StartSheet({required this.repo});
  final AudioRepository repo;

  @override
  State<_StartSheet> createState() => _StartSheetState();
}

class _StartSheetState extends State<_StartSheet> {
  final _title = TextEditingController();
  bool _anonymous = false;
  bool _busy = false;

  @override
  void dispose() {
    _title.dispose();
    super.dispose();
  }

  Future<void> _go() async {
    setState(() => _busy = true);
    try {
      final s = await widget.repo.start(_title.text.trim(), anonymous: _anonymous);
      if (mounted) Navigator.pop(context, s);
    } catch (e) {
      if (mounted) {
        showError(context, e);
        setState(() => _busy = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return SafeArea(
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(20, 4, 20, 20 + MediaQuery.viewInsetsOf(context).bottom),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Text('Start an audio room', style: t.textTheme.headlineSmall),
          const SizedBox(height: 6),
          Text('You decide who speaks. Listeners can raise a hand.', style: t.textTheme.bodyMedium),
          const SizedBox(height: 14),
          TextField(key: const Key('audio_title'), controller: _title, maxLength: 80, decoration: const InputDecoration(labelText: 'What is it about?')),
          SwitchListTile(
            key: const Key('audio_anonymous'),
            contentPadding: EdgeInsets.zero,
            value: _anonymous,
            onChanged: (v) => setState(() => _anonymous = v),
            title: const Text('Anonymous lounge'),
            subtitle: const Text('Everyone, you included, appears under a made-up nickname.'),
          ),
          const SizedBox(height: 8),
          FilledButton(
            key: const Key('audio_open'),
            onPressed: _busy ? null : _go,
            child: _busy ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2)) : const Text('Open the room'),
          ),
        ]),
      ),
    );
  }
}
