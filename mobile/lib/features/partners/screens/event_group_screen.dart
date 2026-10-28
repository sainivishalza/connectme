import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/json.dart';
import '../../../core/network/api_client.dart';
import '../../../core/providers.dart';
import '../../../core/realtime/live_refresh.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';

class EventBoard {
  EventBoard({required this.title, required this.going, required this.members, required this.messages});
  final String title;
  final int going;
  final List<String> members;
  final List<EventMessage> messages;
}

class EventMessage {
  EventMessage({required this.id, required this.body, required this.name, required this.mine});
  final String id;
  final String body;
  final String name;
  final bool mine;

  factory EventMessage.fromJson(Map<String, dynamic> j) =>
      EventMessage(id: jsonString(j['id']), body: jsonString(j['body']), name: jsonString(j['name']), mine: jsonBool(j['mine']));
}

class EventGroupRepository {
  EventGroupRepository(this._api);
  final ApiClient _api;

  Future<EventBoard> board(String eventId) async {
    final m = await _api.getMap('/eventgroups/$eventId');
    return EventBoard(
      title: jsonString(m['title']),
      going: jsonInt(m['going']),
      members: jsonMaps(m['members']).map((x) => jsonString(x['name'])).toList(),
      messages: listOf(m['messages'], EventMessage.fromJson),
    );
  }

  Future<void> post(String eventId, String body) => _api.post('/eventgroups/$eventId/messages', body: {'body': body});
  Future<void> delete(String id) => _api.delete('/eventgroups/messages/$id');
  Future<void> report(String id) => _api.post('/eventgroups/messages/$id/report', body: {'reason': 'inappropriate_content'});
}

final eventGroupRepositoryProvider = Provider<EventGroupRepository>((ref) => EventGroupRepository(ref.watch(apiClientProvider)));

final eventBoardProvider = FutureProvider.autoDispose.family<EventBoard, String>((ref, id) => ref.watch(eventGroupRepositoryProvider).board(id));

/// The board for one event: who is going, and a place to talk until the day
/// after. Only people with a spot can open it.
class EventGroupScreen extends ConsumerStatefulWidget {
  const EventGroupScreen({super.key, required this.eventId});
  final String eventId;

  @override
  ConsumerState<EventGroupScreen> createState() => _EventGroupScreenState();
}

class _EventGroupScreenState extends ConsumerState<EventGroupScreen> {
  LiveRefresh? _live;
  final _text = TextEditingController();
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    // Other members' messages arrive by signal, with a slow refresh as backup.
    _live = LiveRefresh.of(ref, kind: 'event', id: widget.eventId, interval: const Duration(seconds: 15), onTick: () => ref.invalidate(eventBoardProvider(widget.eventId))).start();
  }

  @override
  void dispose() {
    _live?.dispose();
    _text.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    final body = _text.text.trim();
    if (body.isEmpty || _busy) return;
    setState(() => _busy = true);
    final ok = await guarded(context, () => ref.read(eventGroupRepositoryProvider).post(widget.eventId, body));
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      _text.clear();
      ref.invalidate(eventBoardProvider(widget.eventId));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final v = ref.watch(eventBoardProvider(widget.eventId));
    final repo = ref.read(eventGroupRepositoryProvider);
    return Scaffold(
      appBar: AppBar(title: Text(v.asData?.value.title ?? 'Event group', overflow: TextOverflow.ellipsis)),
      body: v.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(eventBoardProvider(widget.eventId))),
        data: (b) => Column(children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
            child: Align(
              alignment: Alignment.centerLeft,
              child: Text(
                '${b.going} going${b.members.isEmpty ? '' : ': ${b.members.take(6).join(', ')}${b.members.length > 6 ? ' and more' : ''}'}',
                key: const Key('going_line'),
                style: t.textTheme.bodySmall,
              ),
            ),
          ),
          Expanded(
            child: b.messages.isEmpty
                ? const EmptyState(icon: Icons.forum_outlined, title: 'Say hello', message: 'Only people going to this event can see this board. It closes the day after.')
                : ListView(padding: const EdgeInsets.symmetric(horizontal: 16), children: [
                    for (final m in b.messages)
                      ListTile(
                        key: Key('msg_${m.id}'),
                        contentPadding: EdgeInsets.zero,
                        title: Text(m.mine ? 'You' : m.name, style: t.textTheme.titleSmall),
                        subtitle: Text(m.body),
                        trailing: IconButton(
                          key: Key(m.mine ? 'delete_${m.id}' : 'report_${m.id}'),
                          tooltip: m.mine ? 'Delete' : 'Report',
                          icon: Icon(m.mine ? Icons.delete_outline : Icons.flag_outlined),
                          onPressed: () async {
                            final ok = await guarded(context, () => m.mine ? repo.delete(m.id) : repo.report(m.id));
                            if (!ok) return;
                            ref.invalidate(eventBoardProvider(widget.eventId));
                            if (!m.mine && context.mounted) showMessage(context, 'Reported. Thank you.');
                          },
                        ),
                      ),
                  ]),
          ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 4, 8, 8),
              child: Row(children: [
                Expanded(child: TextField(key: const Key('event_text'), controller: _text, maxLength: 280, decoration: const InputDecoration(hintText: 'Message the group', counterText: ''), onSubmitted: (_) => _send())),
                IconButton.filled(key: const Key('event_send'), tooltip: 'Send', onPressed: _busy ? null : _send, icon: const Icon(Icons.send)),
              ]),
            ),
          ),
        ]),
      ),
    );
  }
}
