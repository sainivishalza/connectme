import 'package:flutter/material.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../social_repository.dart';
import 'post_card.dart';

/// The replies under one post, with a box to add yours. Under an anonymous
/// post everyone, the author included, is shown as a nickname.
class CommentsSheet extends StatefulWidget {
  const CommentsSheet({
    super.key,
    required this.load,
    required this.send,
    required this.delete,
    required this.report,
    required this.canModerate,
    this.onCount,
  });

  /// What the sheet needs from whatever the comments are under (a post or a video).
  final Future<List<PostComment>> Function() load;
  final Future<void> Function(String body) send;
  final Future<void> Function(String commentId) delete;
  final Future<void> Function(String commentId) report;

  /// True when the viewer owns the thing commented on and may delete any comment.
  final bool canModerate;

  /// Called with the new comment count after the list changes.
  final void Function(int count)? onCount;

  @override
  State<CommentsSheet> createState() => _CommentsSheetState();
}

class _CommentsSheetState extends State<CommentsSheet> {
  final _text = TextEditingController();
  List<PostComment>? _items;
  Object? _error;
  bool _sending = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final items = await widget.load();
      if (!mounted) return;
      setState(() {
        _items = items;
        _error = null;
      });
      widget.onCount?.call(items.length);
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _send() async {
    final body = _text.text.trim();
    if (body.length < 3 || _sending) return;
    setState(() => _sending = true);
    final ok = await guarded(context, () => widget.send(body));
    if (!mounted) return;
    setState(() => _sending = false);
    if (ok) {
      _text.clear();
      await _load();
    }
  }

  Future<void> _delete(PostComment c) async {
    final ok = await guarded(context, () => widget.delete(c.id));
    if (ok) await _load();
  }

  Future<void> _report(PostComment c) async {
    final ok = await guarded(context, () => widget.report(c.id));
    if (ok && mounted) showMessage(context, 'Reported. Thank you.');
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final items = _items;
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.of(context).viewInsets.bottom),
      child: SizedBox(
        height: MediaQuery.of(context).size.height * 0.7,
        child: Column(children: [
          Padding(padding: const EdgeInsets.all(16), child: Text('Comments', style: t.textTheme.titleMedium)),
          Expanded(
            child: _error != null
                ? ErrorView(error: _error!, onRetry: _load)
                : items == null
                    ? const Loader()
                    : items.isEmpty
                        ? const EmptyState(title: 'No comments yet', message: 'Be the first to reply.', icon: Icons.mode_comment_outlined)
                        : ListView.builder(
                            padding: const EdgeInsets.symmetric(horizontal: 16),
                            itemCount: items.length,
                            itemBuilder: (c, i) {
                              final x = items[i];
                              return ListTile(
                                key: Key('comment_${x.id}'),
                                contentPadding: EdgeInsets.zero,
                                title: Row(children: [
                                  Flexible(child: Text(x.mine ? 'You' : x.name, style: t.textTheme.titleSmall, overflow: TextOverflow.ellipsis)),
                                  if (x.author) ...[const SizedBox(width: 6), const Chip(label: Text('Author'), visualDensity: VisualDensity.compact)],
                                  if (x.createdAt != null) ...[const SizedBox(width: 8), Text(timeAgo(x.createdAt!), style: t.textTheme.bodySmall)],
                                ]),
                                subtitle: Text(x.body),
                                trailing: (x.mine || widget.canModerate)
                                    ? IconButton(key: Key('delete_comment_${x.id}'), icon: const Icon(Icons.delete_outline), onPressed: () => _delete(x))
                                    : IconButton(key: Key('report_comment_${x.id}'), icon: const Icon(Icons.flag_outlined), onPressed: () => _report(x)),
                              );
                            },
                          ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 8, 12),
            child: Row(children: [
              Expanded(
                child: TextField(
                  key: const Key('comment_text'),
                  controller: _text,
                  maxLength: 280,
                  minLines: 1,
                  maxLines: 3,
                  decoration: const InputDecoration(hintText: 'Add a comment', counterText: ''),
                  onChanged: (_) => setState(() {}),
                ),
              ),
              IconButton.filled(
                key: const Key('send_comment'),
                onPressed: _text.text.trim().length < 3 || _sending ? null : _send,
                icon: const Icon(Icons.send),
              ),
            ]),
          ),
        ]),
      ),
    );
  }
}
