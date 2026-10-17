import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../social/widgets/post_card.dart' show timeAgo;
import '../forums_repository.dart';

/// The threads on one board, most recently active first.
class BoardScreen extends ConsumerStatefulWidget {
  const BoardScreen({super.key, required this.board});
  final Board board;

  @override
  ConsumerState<BoardScreen> createState() => _BoardScreenState();
}

class _BoardScreenState extends ConsumerState<BoardScreen> {
  late final ForumsRepository _repo;
  List<ForumThread> _threads = [];
  Object? _error;
  bool _loading = true;
  bool _more = true;
  bool _loadingMore = false;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(forumsRepositoryProvider);
    _load();
  }

  Future<void> _load() async {
    try {
      final ts = await _repo.threads(widget.board.slug);
      if (mounted) {
        setState(() {
          _threads = ts;
          _more = ts.length >= 20;
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

  Future<void> _loadMore() async {
    if (!_more || _loadingMore || _threads.isEmpty) return;
    _loadingMore = true;
    try {
      final ts = await _repo.threads(widget.board.slug, before: _threads.last.lastReplyAt);
      if (mounted) {
        setState(() {
          _threads = [..._threads, ...ts];
          _more = ts.length >= 20;
        });
      }
    } catch (_) {}
    _loadingMore = false;
  }

  Future<void> _start() async {
    final made = await showModalBottomSheet<ForumThread>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _NewThreadSheet(repo: _repo, slug: widget.board.slug),
    );
    if (made != null && mounted) {
      await context.push('/thread/${made.id}');
      if (mounted) _load();
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    Widget body;
    if (_loading) {
      body = const Loader();
    } else if (_error != null) {
      body = ErrorView(error: _error!, onRetry: () {
        setState(() => _loading = true);
        _load();
      });
    } else if (_threads.isEmpty) {
      body = EmptyState(
        icon: Icons.forum_outlined,
        title: 'Nothing here yet',
        message: 'Start the first conversation on this board.',
        action: FilledButton(onPressed: _start, child: const Text('Start a thread')),
      );
    } else {
      body = NotificationListener<ScrollNotification>(
        onNotification: (n) {
          if (n.metrics.pixels > n.metrics.maxScrollExtent - 300) _loadMore();
          return false;
        },
        child: RefreshIndicator(
          onRefresh: _load,
          child: ListView.separated(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 96),
            itemCount: _threads.length,
            separatorBuilder: (_, _) => const SizedBox(height: 10),
            itemBuilder: (c, i) {
              final th = _threads[i];
              return Card(
                child: ListTile(
                  key: Key('thread_${th.id}'),
                  title: Text(th.title, maxLines: 2, overflow: TextOverflow.ellipsis),
                  subtitle: Text('${th.mine ? 'You' : th.author} · ${th.replies} ${th.replies == 1 ? 'reply' : 'replies'} · ${timeAgo(th.lastReplyAt)}', style: t.textTheme.bodySmall),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () async {
                    await c.push('/thread/${th.id}');
                    if (mounted) _load();
                  },
                ),
              );
            },
          ),
        ),
      );
    }
    return Scaffold(
      appBar: AppBar(title: Text(widget.board.title)),
      floatingActionButton: FloatingActionButton.extended(key: const Key('new_thread'), onPressed: _start, icon: const Icon(Icons.edit_outlined), label: const Text('New thread')),
      body: body,
    );
  }
}

class _NewThreadSheet extends StatefulWidget {
  const _NewThreadSheet({required this.repo, required this.slug});
  final ForumsRepository repo;
  final String slug;

  @override
  State<_NewThreadSheet> createState() => _NewThreadSheetState();
}

class _NewThreadSheetState extends State<_NewThreadSheet> {
  final _title = TextEditingController();
  final _body = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _title.dispose();
    _body.dispose();
    super.dispose();
  }

  Future<void> _post() async {
    setState(() => _busy = true);
    try {
      final th = await widget.repo.createThread(widget.slug, title: _title.text.trim(), body: _body.text.trim());
      if (mounted) Navigator.pop(context, th);
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
          Text('Start a thread', style: t.textTheme.headlineSmall),
          const SizedBox(height: 6),
          Text('Your name shows only while you are in Open mode. Otherwise you appear under a nickname.', style: t.textTheme.bodyMedium),
          const SizedBox(height: 14),
          TextField(key: const Key('thread_title'), controller: _title, maxLength: 120, decoration: const InputDecoration(labelText: 'Title')),
          const SizedBox(height: 8),
          TextField(key: const Key('thread_body'), controller: _body, maxLength: 2000, minLines: 3, maxLines: 6, decoration: const InputDecoration(labelText: 'What do you want to say?')),
          const SizedBox(height: 8),
          FilledButton(
            key: const Key('thread_post'),
            onPressed: _busy ? null : _post,
            child: _busy ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2)) : const Text('Post thread'),
          ),
        ]),
      ),
    );
  }
}
