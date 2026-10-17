import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/states.dart';
import '../forums_repository.dart';

/// Topic boards: dating advice, travel and city boards.
class ForumsScreen extends ConsumerWidget {
  const ForumsScreen({super.key});

  static IconData _icon(String kind) => switch (kind) {
        'travel' => Icons.flight_takeoff,
        'city' => Icons.location_city,
        _ => Icons.favorite_border,
      };

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final boards = ref.watch(forumBoardsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Forums')),
      body: AsyncListView<Board>(
        value: boards,
        onRefresh: () async => ref.refresh(forumBoardsProvider.future),
        emptyTitle: 'No boards yet',
        emptyIcon: Icons.forum_outlined,
        itemBuilder: (c, b) => Card(
          child: ListTile(
            key: Key('board_${b.slug}'),
            leading: CircleAvatar(child: Icon(_icon(b.kind))),
            title: Text(b.title),
            subtitle: Text(b.description.isEmpty ? '${b.threads} threads' : '${b.description}\n${b.threads} ${b.threads == 1 ? 'thread' : 'threads'}'),
            isThreeLine: b.description.isNotEmpty,
            trailing: const Icon(Icons.chevron_right),
            onTap: () async {
              await c.push('/forums/${b.slug}', extra: b);
              ref.invalidate(forumBoardsProvider);
            },
          ),
        ),
      ),
    );
  }
}
