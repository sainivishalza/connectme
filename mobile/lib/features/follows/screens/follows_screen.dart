import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/states.dart';
import '../../discovery/widgets/profile_sheet.dart';
import '../follows_repository.dart';

/// People you follow, and people who follow you (only you can see that list).
class FollowsScreen extends ConsumerWidget {
  const FollowsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return DefaultTabController(
      length: 2,
      child: Scaffold(
        appBar: AppBar(
          title: Text(AppLocalizations.of(context)?.profileFollowing ?? 'Following'),
          bottom: const TabBar(tabs: [Tab(key: Key('tab_following'), text: 'Following'), Tab(key: Key('tab_followers'), text: 'Followers')]),
        ),
        body: const TabBarView(children: [_List('following'), _List('followers')]),
      ),
    );
  }
}

class _List extends ConsumerWidget {
  const _List(this.direction);
  final String direction;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final v = ref.watch(followListProvider(direction));
    final mine = direction == 'following';
    return AsyncListView<PublicProfile>(
      value: v,
      onRefresh: () async => ref.refresh(followListProvider(direction).future),
      emptyTitle: mine ? 'You are not following anyone' : 'No followers yet',
      emptyMessage: mine ? 'Follow people from their profile to hear when they go live.' : 'Go live and share your profile to grow your following.',
      emptyIcon: Icons.people_outline,
      itemBuilder: (c, p) => Card(
        child: ListTile(
          key: Key('follow_${p.ref}'),
          leading: CircleAvatar(child: Text(p.title.characters.first.toUpperCase())),
          title: Text(p.title, maxLines: 1, overflow: TextOverflow.ellipsis),
          subtitle: Text(p.charmLevel >= 2 ? 'Charm Lv ${p.charmLevel}' : (p.intro.isEmpty ? '' : p.intro), maxLines: 1, overflow: TextOverflow.ellipsis),
          onTap: () => showProfileSheet(c, p),
        ),
      ),
    );
  }
}
