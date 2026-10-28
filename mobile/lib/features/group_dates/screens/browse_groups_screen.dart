import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../groups_repository.dart';
import '../widgets/group_card.dart';

/// Groups the caller's group could match with.
class BrowseGroupsScreen extends ConsumerStatefulWidget {
  const BrowseGroupsScreen({super.key, required this.groupId});
  final String groupId;
  @override
  ConsumerState<BrowseGroupsScreen> createState() => _BrowseGroupsScreenState();
}

class _BrowseGroupsScreenState extends ConsumerState<BrowseGroupsScreen> {
  String? _activity;
  final int _radius = 25;
  Future<List<Group>>? _f;
  final _liked = <String>{};

  @override
  void initState() {
    super.initState();
    _load();
  }

  void _load() => _f = ref.read(groupsRepositoryProvider).browse(widget.groupId, activity: _activity, radiusKm: _radius);

  Future<void> _match(Group g) async {
    String? gm;
    final ok = await guarded(context, () async => gm = await ref.read(groupsRepositoryProvider).match(widget.groupId, g.id));
    if (!ok || !mounted) return;
    setState(() => _liked.add(g.id));
    if (gm != null) {
      ref.invalidate(groupMatchesProvider);
      await showDialog<void>(
        context: context,
        builder: (c) => AlertDialog(
          icon: const Icon(Icons.celebration, size: 40),
          title: const Text("It's a group match!"),
          content: Text('You and ${g.name} are matched. Say hello and plan your date.'),
          actions: [
            TextButton(onPressed: () => Navigator.pop(c), child: Text(AppLocalizations.of(context)?.sLater ?? 'Later')),
            FilledButton(
              onPressed: () {
                Navigator.pop(c);
                context.go('/chats/$gm?group=1');
              },
              child: Text(AppLocalizations.of(context)?.sOpenChat ?? 'Open chat'),
            ),
          ],
        ),
      );
    } else {
      showMessage(context, 'Interest sent. It becomes a match when everyone in both groups agrees.');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.sGroupsNearby ?? 'Groups nearby')),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(12),
            child: ActivityPicker(value: _activity, allowNone: true, onChanged: (v) => setState(() {
              _activity = v;
              _load();
            })),
          ),
          Expanded(
            child: FutureBuilder<List<Group>>(
              future: _f,
              builder: (c, s) {
                if (s.connectionState != ConnectionState.done) return const Loader();
                if (s.hasError) return ErrorView(error: s.error!, onRetry: () => setState(_load));
                final list = s.data ?? [];
                if (list.isEmpty) {
                  return const EmptyState(
                    icon: Icons.search_off,
                    title: 'No groups nearby',
                    message: 'Only complete groups of the same type are shown.',
                  );
                }
                return ListView(
                  padding: const EdgeInsets.all(16),
                  children: [
                    for (final g in list)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: GroupCard(
                          group: g,
                          trailing: FilledButton(
                            key: Key('match_${g.id}'),
                            onPressed: _liked.contains(g.id) ? null : () => _match(g),
                            style: FilledButton.styleFrom(minimumSize: const Size(80, 40)),
                            child: Text(_liked.contains(g.id) ? 'Sent' : 'Match'),
                          ),
                        ),
                      ),
                  ],
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}
