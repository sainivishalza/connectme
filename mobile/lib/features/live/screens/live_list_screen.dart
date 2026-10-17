import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/states.dart';
import '../live_repository.dart';

/// Who is live right now, with a button to start your own stream.
class LiveListScreen extends ConsumerWidget {
  const LiveListScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final rooms = ref.watch(liveRoomsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Live')),
      floatingActionButton: FloatingActionButton.extended(
        key: const Key('go_live'),
        onPressed: () => context.push('/live/broadcast'),
        icon: const Icon(Icons.videocam),
        label: const Text('Go live'),
      ),
      body: AsyncListView<LiveRoom>(
        value: rooms,
        onRefresh: () async => ref.refresh(liveRoomsProvider.future),
        emptyTitle: 'Nobody is live',
        emptyMessage: 'Be the first: start a stream and say hi.',
        emptyIcon: Icons.live_tv_outlined,
        itemBuilder: (c, r) => Card(
          child: ListTile(
            key: Key('room_${r.id}'),
            leading: const CircleAvatar(child: Icon(Icons.sensors)),
            title: Text(r.title, maxLines: 1, overflow: TextOverflow.ellipsis),
            subtitle: Text(r.mine ? 'You' : r.hostName),
            trailing: const Chip(label: Text('LIVE')),
            onTap: () => c.push('/live/${r.id}', extra: r),
          ),
        ),
      ),
    );
  }
}
