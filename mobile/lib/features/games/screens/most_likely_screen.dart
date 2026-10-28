import 'dart:async';

import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/json.dart';
import '../../../core/network/api_client.dart';
import '../../../core/providers.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../../core/realtime/live_refresh.dart';

class MltCandidate {
  MltCandidate({required this.id, required this.label, this.count});
  final String id;
  final String label;
  final int? count;
}

class MltState {
  MltState({
    required this.active,
    this.pollId = '',
    this.prompt = '',
    this.closed = false,
    this.members = 0,
    this.voted = 0,
    this.myVote = '',
    this.candidates = const [],
  });
  final bool active;
  final String pollId;
  final String prompt;
  final bool closed;
  final int members;
  final int voted;
  final String myVote;
  final List<MltCandidate> candidates;

  factory MltState.fromJson(Map<String, dynamic> j) => MltState(
    active: jsonBool(j['active']),
    pollId: jsonString(j['poll_id']),
    prompt: jsonString(j['prompt']),
    closed: jsonBool(j['closed']),
    members: jsonInt(j['members']),
    voted: jsonInt(j['voted']),
    myVote: jsonString(j['my_vote']),
    candidates: jsonMaps(j['candidates'])
        .map(
          (c) => MltCandidate(
            id: jsonString(c['id']),
            label: jsonString(c['label']),
            count: c['count'] is num ? jsonInt(c['count']) : null,
          ),
        )
        .toList(),
  );
}

class MltRepository {
  MltRepository(this._api);
  final ApiClient _api;
  Future<MltState> state(String gm) async =>
      MltState.fromJson(await _api.getMap('/games/group/$gm/mlt'));
  Future<MltState> start(String gm) async =>
      MltState.fromJson(await _api.postMap('/games/group/$gm/mlt/start'));
  Future<MltState> vote(String gm, String pollId, String candidate) async =>
      MltState.fromJson(
        await _api.postMap(
          '/games/group/$gm/mlt/vote',
          body: {'poll_id': pollId, 'candidate': candidate},
        ),
      );
}

final mltRepositoryProvider = Provider<MltRepository>(
  (ref) => MltRepository(ref.watch(apiClientProvider)),
);

/// "Most likely to" for a matched group chat: pick who fits the prompt. You
/// see how many picked each person once you have voted, never who picked whom.
class MostLikelyScreen extends ConsumerStatefulWidget {
  const MostLikelyScreen({
    super.key,
    required this.groupMatchId,
    this.pollInterval = const Duration(seconds: 3),
  });
  final String groupMatchId;
  final Duration pollInterval;

  @override
  ConsumerState<MostLikelyScreen> createState() => _MostLikelyScreenState();
}

class _MostLikelyScreenState extends ConsumerState<MostLikelyScreen> {
  late final MltRepository _repo;
  MltState? _state;
  Object? _error;
  LiveRefresh? _live;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(mltRepositoryProvider);
    _load();
    _live = LiveRefresh.of(ref, kind: 'game', id: widget.groupMatchId, interval: widget.pollInterval, onTick: _poll).start();
  }

  @override
  void dispose() {
    _live?.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final s = await _repo.state(widget.groupMatchId);
      if (mounted) {
        setState(() {
          _state = s;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _poll() async {
    if (_busy || !mounted) return;
    try {
      final s = await _repo.state(widget.groupMatchId);
      if (mounted) setState(() => _state = s);
    } catch (_) {}
  }

  Future<void> _act(Future<MltState> Function() f) async {
    setState(() => _busy = true);
    try {
      final s = await f();
      if (mounted) setState(() => _state = s);
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final s = _state;
    Widget body;
    if (_error != null) {
      body = ErrorView(error: _error!, onRetry: _load);
    } else if (s == null) {
      body = const Loader();
    } else if (!s.active) {
      body = EmptyState(
        icon: Icons.emoji_people_outlined,
        title: 'Most likely to',
        message: 'A prompt appears and everyone picks who in the chat fits it best. You only see the totals.',
        action: FilledButton(
          key: const Key('mlt_start'),
          onPressed: _busy
              ? null
              : () => _act(() => _repo.start(widget.groupMatchId)),
          child: Text(AppLocalizations.of(context)?.sStartARound ?? 'Start a round'),
        ),
      );
    } else {
      final voted = s.myVote.isNotEmpty;
      final top = s.candidates.fold<int>(
        0,
        (m, c) => (c.count ?? 0) > m ? c.count! : m,
      );
      body = ListView(
        padding: const EdgeInsets.all(20),
        children: [
          Text(
            s.closed
                ? 'Round finished'
                : '${s.voted} of ${s.members} have voted',
            key: const Key('mlt_status'),
            style: t.textTheme.labelLarge,
          ),
          const SizedBox(height: 10),
          Text(
            s.prompt,
            key: const Key('mlt_prompt'),
            style: t.textTheme.headlineSmall,
          ),
          const SizedBox(height: 14),
          for (final c in s.candidates)
            Card(
              key: Key('mlt_${c.id}'),
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(16),
                side: BorderSide(
                  color: s.myVote == c.id
                      ? t.colorScheme.primary
                      : t.colorScheme.outlineVariant,
                  width: s.myVote == c.id ? 2 : 1,
                ),
              ),
              child: ListTile(
                title: Text(c.label),
                subtitle: c.count == null
                    ? null
                    : Text(
                        '${c.count} ${c.count == 1 ? 'vote' : 'votes'}${s.myVote == c.id ? ' - your pick' : ''}',
                      ),
                trailing:
                    c.count != null && s.closed && top > 0 && c.count == top
                    ? const Icon(Icons.emoji_events_outlined)
                    : null,
                onTap: (!voted && !s.closed && !_busy)
                    ? () => _act(
                        () => _repo.vote(widget.groupMatchId, s.pollId, c.id),
                      )
                    : null,
              ),
            ),
          const SizedBox(height: 12),
          if (!voted && !s.closed)
            Text(
              'Pick someone to see how everyone voted.',
              style: t.textTheme.bodySmall,
            ),
          if (s.closed)
            FilledButton(
              key: const Key('mlt_next'),
              onPressed: _busy
                  ? null
                  : () => _act(() => _repo.start(widget.groupMatchId)),
              child: Text(AppLocalizations.of(context)?.sNextPrompt ?? 'Next prompt'),
            ),
        ],
      );
    }
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.sMostLikelyTo ?? 'Most likely to')),
      body: body,
    );
  }
}
