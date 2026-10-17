import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/network/api_client.dart';
import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../communities_repository.dart';

/// Nearby groups: interest communities around you, and the community of any
/// hotel you are checked in at.
class CommunitiesScreen extends ConsumerStatefulWidget {
  const CommunitiesScreen({super.key});

  @override
  ConsumerState<CommunitiesScreen> createState() => _CommunitiesScreenState();
}

class _CommunitiesScreenState extends ConsumerState<CommunitiesScreen> {
  late final CommunitiesRepository _repo;
  List<Community> _items = [];
  Object? _error;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(communitiesRepositoryProvider);
    _load();
  }

  Future<void> _load() async {
    try {
      final cs = await _repo.nearby();
      if (mounted) {
        setState(() {
          _items = cs;
          _error = null;
          _loading = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e;
          _loading = false;
        });
      }
    }
  }

  Future<void> _open(Community c) async {
    await context.push('/community/${c.id}', extra: c);
    _load();
  }

  Future<void> _join(Community c) async {
    try {
      final joined = await _repo.join(c.id);
      if (mounted) await _open(joined);
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _create() async {
    final made = await showModalBottomSheet<Community>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _CreateSheet(repo: _repo),
    );
    if (made != null && mounted) {
      await _load();
      if (mounted) await _open(made);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    Widget body;
    if (_loading) {
      body = const Loader();
    } else if (_error != null) {
      final e = _error;
      body = e is ApiException && e.status == 422
          ? EmptyState(
              icon: Icons.my_location,
              title: 'Share your location first',
              message: e.message,
              action: OutlinedButton(onPressed: () => context.push('/profile/edit'), child: const Text('Update my location')),
            )
          : ErrorView(error: e!, onRetry: () {
              setState(() => _loading = true);
              _load();
            });
    } else if (_items.isEmpty) {
      body = RefreshIndicator(
        onRefresh: _load,
        child: ListView(children: [
          SizedBox(
            height: 420,
            child: EmptyState(
              icon: Icons.groups_2_outlined,
              title: 'No groups near you yet',
              message: 'Start one for something you enjoy and people around you can join.',
              action: FilledButton(onPressed: _create, child: const Text('Start a group')),
            ),
          ),
        ]),
      );
    } else {
      body = RefreshIndicator(
        onRefresh: _load,
        child: ListView.separated(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 96),
          itemCount: _items.length,
          separatorBuilder: (_, _) => const SizedBox(height: 12),
          itemBuilder: (c, i) {
            final g = _items[i];
            final light = g.isHotel ? ModeLight.lamplight : ModeLight.daylight;
            return Card(
              clipBehavior: Clip.antiAlias,
              child: InkWell(
                key: Key('community_${g.id}'),
                onTap: g.joined ? () => _open(g) : null,
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Row(
                    children: [
                      Container(
                        width: 52,
                        height: 52,
                        decoration: BoxDecoration(gradient: light.gradient, borderRadius: BorderRadius.circular(Radii.control)),
                        child: Icon(g.isHotel ? Icons.hotel_outlined : Icons.groups_2_outlined, color: light.ink),
                      ),
                      const SizedBox(width: 14),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(g.name, style: t.textTheme.titleMedium),
                            const SizedBox(height: 2),
                            Wrap(spacing: 12, children: [
                              Text('${g.members} ${g.members == 1 ? 'member' : 'members'}', style: t.textTheme.bodySmall),
                              if (g.distance.isNotEmpty) Text(g.distance, style: t.textTheme.bodySmall),
                              if (g.tag.isNotEmpty) Text(g.tag, style: t.textTheme.bodySmall),
                            ]),
                            if (g.description.isNotEmpty) ...[const SizedBox(height: 6), Text(g.description, style: t.textTheme.bodyMedium)],
                          ],
                        ),
                      ),
                      const SizedBox(width: 8),
                      g.joined
                          ? const Icon(Icons.chevron_right)
                          : FilledButton.tonal(
                              key: Key('join_${g.id}'),
                              onPressed: () => _join(g),
                              style: FilledButton.styleFrom(minimumSize: const Size(0, 40)),
                              child: const Text('Join'),
                            ),
                    ],
                  ),
                ),
              ),
            );
          },
        ),
      );
    }
    return Scaffold(
      appBar: AppBar(title: const Text('Nearby groups')),
      floatingActionButton: FloatingActionButton.extended(
        key: const Key('new_community'),
        onPressed: _create,
        icon: const Icon(Icons.add),
        label: const Text('Start a group'),
      ),
      body: body,
    );
  }
}

class _CreateSheet extends StatefulWidget {
  const _CreateSheet({required this.repo});
  final CommunitiesRepository repo;

  @override
  State<_CreateSheet> createState() => _CreateSheetState();
}

class _CreateSheetState extends State<_CreateSheet> {
  final _name = TextEditingController();
  final _about = TextEditingController();
  final _tag = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _name.dispose();
    _about.dispose();
    _tag.dispose();
    super.dispose();
  }

  Future<void> _make() async {
    setState(() => _busy = true);
    try {
      final c = await widget.repo.create(name: _name.text.trim(), description: _about.text.trim(), tag: _tag.text.trim());
      if (mounted) Navigator.pop(context, c);
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
          Text('Start a group', style: t.textTheme.headlineSmall),
          const SizedBox(height: 6),
          Text('People within about 25 km can find and join it. You can post to it and remove posts you do not want.', style: t.textTheme.bodyMedium),
          const SizedBox(height: 14),
          TextField(key: const Key('community_name'), controller: _name, maxLength: 60, decoration: const InputDecoration(labelText: 'Name')),
          const SizedBox(height: 8),
          TextField(key: const Key('community_about'), controller: _about, maxLength: 200, minLines: 2, maxLines: 3, decoration: const InputDecoration(labelText: 'What is it about? (optional)')),
          const SizedBox(height: 8),
          TextField(key: const Key('community_tag'), controller: _tag, maxLength: 30, decoration: const InputDecoration(labelText: 'Interest, e.g. hiking (optional)')),
          const SizedBox(height: 8),
          FilledButton(
            key: const Key('community_create'),
            onPressed: _busy ? null : _make,
            child: _busy ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2)) : const Text('Create group'),
          ),
        ]),
      ),
    );
  }
}
