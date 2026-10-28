import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../chat_providers.dart';
import '../data/chat_models.dart';
import '../data/chat_repository.dart';
import '../data/message_search.dart';

/// Search the messages on this device: in every chat, or in one ([convId]).
/// Chats are end-to-end encrypted, so only what this device already holds can
/// be found; the screen says so.
class MessageSearchScreen extends ConsumerStatefulWidget {
  const MessageSearchScreen({super.key, this.convId});

  /// When set, searches only this conversation and returns the chosen
  /// message's id to the chat (so it can scroll there).
  final String? convId;

  @override
  ConsumerState<MessageSearchScreen> createState() => _MessageSearchScreenState();
}

class _MessageSearchScreenState extends ConsumerState<MessageSearchScreen> {
  final _q = TextEditingController();
  Timer? _debounce;
  List<SearchHit> _hits = const [];
  bool _searched = false;
  Map<String, bool> _groups = {};

  @override
  void dispose() {
    _debounce?.cancel();
    _q.dispose();
    super.dispose();
  }

  void _onChanged(String v) {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 250), _run);
  }

  Future<void> _run() async {
    final cache = ref.read(messageCacheProvider);
    if (cache == null) return;
    final convs = await ref.read(conversationsProvider.future).catchError((_) => <Conversation>[]);
    final titles = {
      for (final c in convs)
        if (widget.convId == null || c.id == widget.convId) c.id: c.title,
    };
    _groups = {for (final c in convs) c.id: c.isGroup};
    final hits = await MessageSearch(cache).search(_q.text, titles);
    if (!mounted) return;
    setState(() {
      _hits = hits;
      _searched = _q.text.trim().length >= 2;
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        titleSpacing: 0,
        title: TextField(
          key: const Key('search_field'),
          controller: _q,
          autofocus: true,
          onChanged: _onChanged,
          textInputAction: TextInputAction.search,
          decoration: InputDecoration(
            hintText: widget.convId == null ? 'Search all your chats' : 'Search this chat',
            border: InputBorder.none,
            enabledBorder: InputBorder.none,
            focusedBorder: InputBorder.none,
            filled: false,
          ),
        ),
      ),
      body: !_searched
          ? Center(
              child: Padding(
                padding: const EdgeInsets.all(32),
                child: Column(mainAxisSize: MainAxisSize.min, children: [
                  Icon(Icons.search, size: 48, color: t.colorScheme.onSurfaceVariant),
                  const SizedBox(height: 12),
                  Text('Type at least two letters', style: t.textTheme.titleMedium),
                  const SizedBox(height: 6),
                  Text(
                    'Your chats are end-to-end encrypted, so search only looks at messages already on this device.',
                    textAlign: TextAlign.center,
                    style: t.textTheme.bodySmall?.copyWith(color: t.colorScheme.onSurfaceVariant),
                  ),
                ]),
              ),
            )
          : _hits.isEmpty
              ? Center(child: Text('No messages found', key: const Key('no_results'), style: t.textTheme.titleMedium))
              : ListView.separated(
                  itemCount: _hits.length,
                  separatorBuilder: (_, _) => const Divider(height: 1),
                  itemBuilder: (c, i) {
                    final h = _hits[i];
                    return ListTile(
                      key: Key('hit_$i'),
                      title: widget.convId == null ? Text(h.convTitle, style: t.textTheme.titleSmall) : null,
                      subtitle: _Snippet(hit: h),
                      trailing: Text(_when(h.message.createdAt), style: t.textTheme.labelSmall),
                      onTap: () {
                        if (widget.convId != null) {
                          context.pop(h.message.id);
                        } else {
                          context.pushReplacement('/chats/${h.convId}?group=${_groups[h.convId] == true ? 1 : 0}&focus=${h.message.id}');
                        }
                      },
                    );
                  },
                ),
    );
  }

  static String _when(DateTime d) {
    final now = DateTime.now();
    if (d.year == now.year && d.month == now.month && d.day == now.day) {
      return '${d.hour.toString().padLeft(2, '0')}:${d.minute.toString().padLeft(2, '0')}';
    }
    return '${d.day}/${d.month}/${d.year}';
  }
}

/// The message text with the matched part in bold, trimmed around the match.
class _Snippet extends StatelessWidget {
  const _Snippet({required this.hit});
  final SearchHit hit;

  @override
  Widget build(BuildContext context) {
    final t = hit.message.text;
    final from = hit.start > 30 ? hit.start - 30 : 0;
    final to = hit.end + 60 < t.length ? hit.end + 60 : t.length;
    final style = Theme.of(context).textTheme.bodyMedium;
    return Text.rich(
      TextSpan(style: style, children: [
        if (from > 0) const TextSpan(text: '…'),
        TextSpan(text: t.substring(from, hit.start)),
        TextSpan(text: t.substring(hit.start, hit.end), style: const TextStyle(fontWeight: FontWeight.w800, backgroundColor: Color(0x33FFC107))),
        TextSpan(text: t.substring(hit.end, to)),
        if (to < t.length) const TextSpan(text: '…'),
      ]),
      maxLines: 2,
      overflow: TextOverflow.ellipsis,
    );
  }
}
