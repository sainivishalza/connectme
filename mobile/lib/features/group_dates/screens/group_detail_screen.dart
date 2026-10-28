import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_button.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../discovery/discovery_repository.dart';
import '../groups_repository.dart';
import '../widgets/group_card.dart';

class GroupDetailScreen extends ConsumerWidget {
  const GroupDetailScreen({super.key, required this.groupId});
  final String groupId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final g = ref.watch(groupProvider(groupId));
    final matches = ref.watch(groupMatchesProvider);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.sGroup ?? 'Group')),
      body: g.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(groupProvider(groupId))),
        data: (group) {
          final mine = matches.maybeWhen(data: (l) => l.where((m) => m.myGroupId == groupId).toList(), orElse: () => <GroupMatch>[]);
          return RefreshIndicator(
            onRefresh: () async {
              ref.invalidate(groupMatchesProvider);
              ref.invalidate(groupProvider(groupId));
              await ref.read(groupProvider(groupId).future);
            },
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                GroupCard(group: group),
                const SizedBox(height: 16),
                if (group.locked)
                  const Card(
                    child: ListTile(
                      leading: Icon(Icons.lock_outline),
                      title: Text('Matched'),
                      subtitle: Text('Members are locked while this group has an active match.'),
                    ),
                  ),
                if (!group.locked && !group.ready)
                  AppButton(
                    key: const Key('invite'),
                    label: 'Invite a friend',
                    icon: Icons.person_add_alt,
                    onPressed: () => showModalBottomSheet<void>(
                      context: context, useRootNavigator: true,
                      isScrollControlled: true,
                      showDragHandle: true,
                      builder: (_) => InviteSheet(groupId: groupId),
                    ),
                  ),
                if (group.ready && !group.locked) ...[
                  AppButton(
                    key: const Key('browse'),
                    label: 'Find groups nearby',
                    icon: Icons.search,
                    onPressed: () => context.push('/groups/$groupId/browse'),
                  ),
                ],
                const SizedBox(height: 8),
                OutlinedButton.icon(
                  onPressed: () => _ideas(context, ref),
                  icon: const Icon(Icons.lightbulb_outline),
                  label: Text(AppLocalizations.of(context)?.sDateIdeas ?? 'Date ideas'),
                ),
                if (mine.isNotEmpty) ...[
                  const SizedBox(height: 24),
                  Text('Matched with', style: Theme.of(context).textTheme.titleMedium),
                  for (final m in mine)
                    GroupCard(
                      group: m.other,
                      onTap: () => context.push('/groups/$groupId/dates/${m.id}'),
                      trailing: IconButton(
                        tooltip: 'Open chat',
                        icon: const Icon(Icons.chat_bubble_outline),
                        onPressed: () => context.push('/chats/${m.id}?group=1'),
                      ),
                    ),
                ],
                const SizedBox(height: 24),
                TextButton(
                  key: const Key('leave'),
                  onPressed: () => _leave(context, ref),
                  child: Text('Leave group', style: TextStyle(color: Theme.of(context).colorScheme.error)),
                ),
              ],
            ),
          );
        },
      ),
    );
  }

  Future<void> _ideas(BuildContext context, WidgetRef ref) async {
    Suggestions? s;
    await guarded(context, () async => s = await ref.read(groupsRepositoryProvider).suggestions(groupId));
    if (s == null || !context.mounted) return;
    await showModalBottomSheet<void>(
      context: context, useRootNavigator: true,
      builder: (_) => ListView(
        padding: const EdgeInsets.all(16),
        shrinkWrap: true,
        children: [
          if (s!.plan.isNotEmpty) ...[
            Text('A plan for the day', style: Theme.of(context).textTheme.titleMedium),
            for (var i = 0; i < s!.plan.length; i++)
              ListTile(key: Key('plan_$i'), leading: CircleAvatar(radius: 14, child: Text('${i + 1}')), title: Text(s!.plan[i])),
            const Divider(),
          ],
          Text('Where to go', style: Theme.of(context).textTheme.titleMedium),
          for (final v in s!.venues) ListTile(leading: const Icon(Icons.place_outlined), title: Text(v)),
          const Divider(),
          Text('Icebreakers', style: Theme.of(context).textTheme.titleMedium),
          for (final i in s!.icebreakers) ListTile(leading: const Icon(Icons.chat_outlined), title: Text(i)),
          if (s!.source == 'llm')
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text('Ideas written by an AI assistant from your shared interests. Check details yourself before you go.', key: const Key('ai_note'), style: Theme.of(context).textTheme.bodySmall),
            ),
        ],
      ),
    );
  }

  Future<void> _leave(BuildContext context, WidgetRef ref) async {
    final yes = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Leave this group?'),
        content: const Text('If it has an active match, the match and its chat will end.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: Text(AppLocalizations.of(context)?.sStay ?? 'Stay')),
          TextButton(onPressed: () => Navigator.pop(c, true), child: Text(AppLocalizations.of(context)?.sLeave ?? 'Leave')),
        ],
      ),
    );
    if (yes != true || !context.mounted) return;
    final ok = await guarded(context, () => ref.read(groupsRepositoryProvider).leave(groupId));
    if (ok && context.mounted) {
      ref.invalidate(myGroupsProvider);
      ref.invalidate(groupMatchesProvider);
      context.go('/groups');
    }
  }
}

/// Pick a friend from people nearby (or paste a profile ref) to invite.
class InviteSheet extends ConsumerStatefulWidget {
  const InviteSheet({super.key, required this.groupId});
  final String groupId;
  @override
  ConsumerState<InviteSheet> createState() => _InviteSheetState();
}

class _InviteSheetState extends ConsumerState<InviteSheet> {
  final _ref = TextEditingController();

  @override
  void dispose() {
    _ref.dispose();
    super.dispose();
  }

  Future<void> _invite(String profileRef) async {
    final ok = await guarded(context, () => ref.read(groupsRepositoryProvider).invite(widget.groupId, profileRef));
    if (!ok || !mounted) return;
    ref.invalidate(groupProvider(widget.groupId));
    showMessage(context, 'Invitation sent');
    Navigator.pop(context);
  }

  @override
  Widget build(BuildContext context) {
    final nearby = ref.watch(nearbyProvider);
    return Padding(
      padding: EdgeInsets.fromLTRB(16, 0, 16, MediaQuery.of(context).viewInsets.bottom + 16),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Invite someone', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Row(children: [
            Expanded(child: TextField(key: const Key('invite_ref'), controller: _ref, decoration: const InputDecoration(labelText: 'Profile code'))),
            IconButton(onPressed: () => _invite(_ref.text.trim()), icon: const Icon(Icons.send)),
          ]),
          const SizedBox(height: 12),
          Text('People nearby', style: Theme.of(context).textTheme.titleSmall),
          SizedBox(
            height: 240,
            child: nearby.when(
              loading: () => const Loader(),
              error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(nearbyProvider)),
              data: (list) => list.isEmpty
                  ? const EmptyState(title: 'No one nearby', icon: Icons.people_outline)
                  : ListView(children: [
                      for (final p in list)
                        ListTile(
                          title: Text(p.title),
                          subtitle: Text(p.distance),
                          trailing: const Icon(Icons.person_add_alt),
                          onTap: () => _invite(p.ref),
                        ),
                    ]),
            ),
          ),
        ],
      ),
    );
  }
}
