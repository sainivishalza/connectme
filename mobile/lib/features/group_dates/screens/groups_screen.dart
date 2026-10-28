import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../groups_repository.dart';
import '../widgets/group_card.dart';

/// "Groups" tab: my groups, invitations and my matched group dates.
class GroupsScreen extends ConsumerWidget {
  const GroupsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return DefaultTabController(
      length: 3,
      child: Scaffold(
        appBar: AppBar(
          title: Text(AppLocalizations.of(context)?.sGroupDates ?? 'Group dates'),
          bottom: const TabBar(tabs: [Tab(text: 'My groups'), Tab(text: 'Invites'), Tab(text: 'Matches')]),
        ),
        floatingActionButton: FloatingActionButton.extended(
          key: const Key('new_group'),
          onPressed: () => context.push('/groups/new'),
          icon: const Icon(Icons.add),
          label: Text(AppLocalizations.of(context)?.sNewGroup ?? 'New group'),
        ),
        body: const TabBarView(children: [_MyGroups(), _Invites(), _Matches()]),
      ),
    );
  }
}

class _MyGroups extends ConsumerWidget {
  const _MyGroups();
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final groups = ref.watch(myGroupsProvider);
    return AsyncListView<Group>(
      value: groups,
      onRefresh: () async => ref.refresh(myGroupsProvider.future),
      emptyTitle: 'No groups yet',
      emptyMessage: 'Create a double date (2 people) or a group date (up to 6) and invite friends.',
      emptyIcon: Icons.groups_outlined,
      itemBuilder: (c, g) => GroupCard(group: g, onTap: () => c.push('/groups/${g.id}')),
    );
  }
}

class _Invites extends ConsumerWidget {
  const _Invites();
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final inv = ref.watch(groupInvitesProvider);
    return AsyncListView<GroupInvite>(
      value: inv,
      onRefresh: () async => ref.refresh(groupInvitesProvider.future),
      emptyTitle: 'No invitations',
      emptyIcon: Icons.mail_outline,
      itemBuilder: (c, i) => Card(
        child: ListTile(
          leading: CircleAvatar(child: Icon(GroupCard.iconFor(i.activity))),
          title: Text(i.groupName),
          subtitle: Text('${i.type == 'double' ? 'Double date' : 'Group date'}, ${i.activity.replaceAll('_', ' ')}'),
          trailing: Row(mainAxisSize: MainAxisSize.min, children: [
            IconButton(
              key: Key('decline_${i.id}'),
              tooltip: 'Decline',
              icon: const Icon(Icons.close),
              onPressed: () => _respond(c, ref, i, false),
            ),
            IconButton(
              key: Key('accept_${i.id}'),
              tooltip: 'Join',
              icon: const Icon(Icons.check),
              onPressed: () => _respond(c, ref, i, true),
            ),
          ]),
        ),
      ),
    );
  }

  Future<void> _respond(BuildContext c, WidgetRef ref, GroupInvite i, bool accept) async {
    final ok = await guarded(c, () => ref.read(groupsRepositoryProvider).respond(i.id, accept: accept));
    if (!ok) return;
    ref.invalidate(groupInvitesProvider);
    ref.invalidate(myGroupsProvider);
  }
}

class _Matches extends ConsumerWidget {
  const _Matches();
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final ms = ref.watch(groupMatchesProvider);
    return AsyncListView<GroupMatch>(
      value: ms,
      onRefresh: () async => ref.refresh(groupMatchesProvider.future),
      emptyTitle: 'No group matches yet',
      emptyMessage: 'Open one of your complete groups and browse groups nearby.',
      emptyIcon: Icons.celebration_outlined,
      itemBuilder: (c, m) => GroupCard(
        group: m.other,
        trailing: IconButton(
          tooltip: 'Open chat',
          icon: const Icon(Icons.chat_bubble_outline),
          onPressed: () => c.push('/chats/${m.id}?group=1'),
        ),
        onTap: () => c.push('/groups/${m.myGroupId}'),
      ),
    );
  }
}
