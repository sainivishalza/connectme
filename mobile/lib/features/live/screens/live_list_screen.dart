import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/states.dart';
import '../live_repository.dart';
import '../widgets/room_theme.dart';

/// Who is live right now, with a button to start your own stream.
class LiveListScreen extends ConsumerWidget {
  const LiveListScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final rooms = ref.watch(liveRoomsProvider);
    final filter = ref.watch(liveFilterProvider);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.sLive ?? 'Live')),
      floatingActionButton: FloatingActionButton.extended(
        key: const Key('go_live'),
        onPressed: () => context.push('/live/broadcast'),
        icon: const Icon(Icons.videocam),
        label: Text(AppLocalizations.of(context)?.sGoLive ?? 'Go live'),
      ),
      body: Column(
        children: [
          SizedBox(
            height: 64,
            child: ListView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              children: [
                FilterChip(
                  key: const Key('filter_following'),
                  avatar: const Icon(Icons.person, size: 16),
                  label: Text(AppLocalizations.of(context)?.profileFollowing ?? 'Following'),
                  selected: filter.followingOnly,
                  onSelected: (v) => ref
                      .read(liveFilterProvider.notifier)
                      .set(LiveFilter(topic: filter.topic, followingOnly: v)),
                ),
                for (final t in ['', ...liveTopics])
                  Padding(
                    padding: const EdgeInsets.only(left: 8),
                    child: ChoiceChip(
                      key: Key('topic_${t.isEmpty ? 'all' : t}'),
                      label: Text(topicLabel(t)),
                      selected: filter.topic == t,
                      onSelected: (_) => ref
                          .read(liveFilterProvider.notifier)
                          .set(
                            LiveFilter(
                              topic: t,
                              followingOnly: filter.followingOnly,
                            ),
                          ),
                    ),
                  ),
              ],
            ),
          ),
          Expanded(
            child: AsyncListView<LiveRoom>(
              value: rooms,
              onRefresh: () async => ref.refresh(liveRoomsProvider.future),
              emptyTitle: 'Nobody is live',
              emptyMessage: 'Be the first: start a stream and say hi.',
              emptyIcon: Icons.live_tv_outlined,
              itemBuilder: (c, r) => Card(
                child: ListTile(
                  key: Key('room_${r.id}'),
                  leading: CircleAvatar(
                    backgroundColor: r.theme == 'plain' ? null : RoomTheme.of(r.theme).accent,
                    foregroundColor: r.theme == 'plain' ? null : Colors.white,
                    child: const Icon(Icons.sensors),
                  ),
                  title: Text(
                    r.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  subtitle: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        _subtitle(r),
                        maxLines: 3,
                        overflow: TextOverflow.ellipsis,
                      ),
                      if (r.following)
                        const Padding(
                          padding: EdgeInsets.only(top: 4),
                          child: Chip(
                            key: Key('following_chip'),
                            label: Text('Following'),
                            visualDensity: VisualDensity.compact,
                          ),
                        ),
                    ],
                  ),
                  trailing: const Chip(label: Text('LIVE')),
                  onTap: () => c.push('/live/${r.id}', extra: r),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

String _subtitle(LiveRoom r) {
  final head = '${r.mine ? 'You' : r.hostName} - ${topicLabel(r.topic)}';
  return r.pinned.isEmpty
      ? head
      : [head, r.pinned].join(String.fromCharCode(10));
}
