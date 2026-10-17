import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../social/widgets/post_card.dart' show timeAgo;
import '../forums_repository.dart';

/// One thread: the opening post and its replies, oldest first.
class ThreadScreen extends ConsumerStatefulWidget {
  const ThreadScreen({super.key, required this.threadId});
  final String threadId;

  @override
  ConsumerState<ThreadScreen> createState() => _ThreadScreenState();
}

class _ThreadScreenState extends ConsumerState<ThreadScreen> {
  late final ForumsRepository _repo;
  final _reply = TextEditingController();
  ForumThread? _thread;
  List<ForumReply> _replies = [];
  Object? _error;
  bool _loading = true;
  bool _sending = false;

  static const _reasons = {
    'inappropriate_content': 'Inappropriate content',
    'harassment': 'Harassment',
    'scam': 'Scam',
    'underage': 'Looks underage',
    'spam': 'Spam',
  };

  @override
  void initState() {
    super.initState();
    _repo = ref.read(forumsRepositoryProvider);
    _load();
  }

  @override
  void dispose() {
    _reply.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final th = await _repo.thread(widget.threadId);
      final rs = await _repo.replies(widget.threadId);
      if (mounted) {
        setState(() {
          _thread = th;
          _replies = rs;
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

  Future<void> _send() async {
    final text = _reply.text.trim();
    if (text.length < 2) return;
    setState(() => _sending = true);
    try {
      await _repo.reply(widget.threadId, text);
      _reply.clear();
      await _load();
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _sending = false);
  }

  Future<void> _report(String kind, Object id) async {
    final reason = await showModalBottomSheet<String>(
      context: context,
      builder: (c) => SafeArea(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          const ListTile(title: Text('Report this post')),
          for (final r in _reasons.entries) ListTile(key: Key('report_${r.key}'), title: Text(r.value), onTap: () => Navigator.pop(c, r.key)),
        ]),
      ),
    );
    if (reason == null || !mounted) return;
    if (await guarded(context, () => _repo.report(kind, id, reason)) && mounted) showMessage(context, 'Thanks. Our team will take a look.');
  }

  Future<void> _deleteThread() async {
    if (await guarded(context, () => _repo.deleteThread(widget.threadId)) && mounted) Navigator.of(context).maybePop();
  }

  Future<void> _deleteReply(ForumReply r) async {
    if (await guarded(context, () => _repo.deleteReply(r.id))) _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final th = _thread;
    Widget body;
    if (_loading) {
      body = const Loader();
    } else if (_error != null || th == null) {
      body = ErrorView(error: _error ?? 'missing', onRetry: () {
        setState(() => _loading = true);
        _load();
      });
    } else {
      body = ListView(padding: const EdgeInsets.fromLTRB(16, 8, 16, 16), children: [
        Text(th.title, style: t.textTheme.headlineSmall),
        const SizedBox(height: 4),
        Text('${th.mine ? 'You' : th.author} · ${timeAgo(th.createdAt)}', style: t.textTheme.bodySmall),
        const SizedBox(height: 12),
        Text(th.body, style: t.textTheme.bodyLarge),
        Row(children: [
          const Spacer(),
          if (th.mine)
            TextButton.icon(key: const Key('delete_thread'), onPressed: _deleteThread, icon: const Icon(Icons.delete_outline), label: const Text('Delete'))
          else
            TextButton.icon(key: const Key('report_thread'), onPressed: () => _report('threads', th.id), icon: const Icon(Icons.flag_outlined), label: const Text('Report')),
        ]),
        const Divider(),
        Text('${_replies.length} ${_replies.length == 1 ? 'reply' : 'replies'}', style: t.textTheme.titleMedium),
        const SizedBox(height: 8),
        if (_replies.isEmpty) Text('Be the first to reply.', style: t.textTheme.bodySmall),
        for (final r in _replies)
          Padding(
            key: Key('reply_${r.id}'),
            padding: const EdgeInsets.only(bottom: 14),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Row(children: [
                Text(r.mine ? 'You' : r.author, style: t.textTheme.labelLarge),
                const SizedBox(width: 8),
                Text(timeAgo(r.createdAt), style: t.textTheme.bodySmall),
                const Spacer(),
                if (r.mine)
                  IconButton(key: Key('delete_reply_${r.id}'), visualDensity: VisualDensity.compact, icon: const Icon(Icons.delete_outline, size: 20), onPressed: () => _deleteReply(r))
                else
                  IconButton(key: Key('report_reply_${r.id}'), visualDensity: VisualDensity.compact, icon: const Icon(Icons.flag_outlined, size: 20), onPressed: () => _report('replies', r.id)),
              ]),
              Text(r.body, style: t.textTheme.bodyMedium),
            ]),
          ),
      ]);
    }
    return Scaffold(
      appBar: AppBar(title: const Text('Thread')),
      body: body,
      bottomNavigationBar: th == null
          ? null
          : SafeArea(
              child: Padding(
                padding: EdgeInsets.fromLTRB(16, 8, 16, 8 + MediaQuery.viewInsetsOf(context).bottom),
                child: Row(children: [
                  Expanded(
                    child: TextField(
                      key: const Key('reply_field'),
                      controller: _reply,
                      maxLength: 1000,
                      minLines: 1,
                      maxLines: 3,
                      decoration: const InputDecoration(hintText: 'Write a reply', counterText: ''),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton.filled(key: const Key('reply_send'), onPressed: _sending ? null : _send, icon: const Icon(Icons.send)),
                ]),
              ),
            ),
    );
  }
}
