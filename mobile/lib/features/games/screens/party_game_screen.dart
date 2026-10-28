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

class PartyPlayer {
  PartyPlayer({required this.label, this.you = false, this.turn = false});
  final String label;
  final bool you;
  final bool turn;
}

class PartyState {
  PartyState({required this.active, this.yourTurn = false, this.phase = '', this.kind = '', this.prompt = '', this.skipsLeft = 2, this.canTakeOver = false, this.players = const []});
  final bool active;
  final bool yourTurn;
  final String phase;
  final String kind;
  final String prompt;
  final int skipsLeft;
  final bool canTakeOver;
  final List<PartyPlayer> players;

  String get turnLabel => players.where((p) => p.turn).map((p) => p.you ? 'You' : p.label).firstOrNull ?? '';

  factory PartyState.fromJson(Map<String, dynamic> j) => PartyState(
        active: jsonBool(j['active']),
        yourTurn: jsonBool(j['your_turn']),
        phase: jsonString(j['phase']),
        kind: jsonString(j['kind']),
        prompt: jsonString(j['prompt']),
        skipsLeft: j['skips_left'] is num ? jsonInt(j['skips_left']) : 2,
        canTakeOver: jsonBool(j['can_take_over']),
        players: jsonMaps(j['players']).map((p) => PartyPlayer(label: jsonString(p['label']), you: jsonBool(p['you']), turn: jsonBool(p['turn']))).toList(),
      );
}

class MaskedAnswer {
  MaskedAnswer({required this.id, required this.alias, required this.kind, required this.prompt, required this.body, required this.mine});
  final String id;
  final String alias;
  final String kind;
  final String prompt;
  final String body;
  final bool mine;

  factory MaskedAnswer.fromJson(Map<String, dynamic> j) => MaskedAnswer(
        id: jsonString(j['id']),
        alias: jsonString(j['alias']),
        kind: jsonString(j['kind']),
        prompt: jsonString(j['prompt']),
        body: jsonString(j['body']),
        mine: jsonBool(j['mine']),
      );
}

class MaskedRoomState {
  MaskedRoomState({required this.game, required this.answers, required this.myAlias, required this.endsAt});
  final PartyState game;
  final List<MaskedAnswer> answers;
  final String myAlias;
  final DateTime endsAt;

  factory MaskedRoomState.fromJson(Map<String, dynamic> j) => MaskedRoomState(
        game: PartyState.fromJson(Map<String, dynamic>.from(j['game'] as Map? ?? {})),
        answers: listOf(j['answers'], MaskedAnswer.fromJson),
        myAlias: jsonString(j['my_alias']),
        endsAt: DateTime.tryParse(jsonString(j['ends_at']))?.toLocal() ?? DateTime.now(),
      );
}

class MaskedStatus {
  MaskedStatus({required this.state, this.roomId = '', this.waiting = 0});
  final String state; // idle | waiting | in_room
  final String roomId;
  final int waiting;

  factory MaskedStatus.fromJson(Map<String, dynamic> j) => MaskedStatus(state: jsonString(j['state']), roomId: jsonString(j['room_id']), waiting: jsonInt(j['waiting']));
}

class PartyRepository {
  PartyRepository(this._api);
  final ApiClient _api;

  Future<PartyState> state(String id) async => PartyState.fromJson(await _api.getMap('/games/party/$id'));
  Future<PartyState> start(String id) async => PartyState.fromJson(await _api.postMap('/games/party/$id/start'));
  Future<PartyState> pick(String id, String kind) async => PartyState.fromJson(await _api.postMap('/games/party/$id/pick', body: {'kind': kind}));
  Future<PartyState> done(String id) async => PartyState.fromJson(await _api.postMap('/games/party/$id/done'));
  Future<PartyState> skip(String id) async => PartyState.fromJson(await _api.postMap('/games/party/$id/skip'));
  Future<PartyState> takeOver(String id) async => PartyState.fromJson(await _api.postMap('/games/party/$id/take-over'));

  Future<MaskedStatus> maskedJoin() async => MaskedStatus.fromJson(await _api.postMap('/games/masked/join'));
  Future<MaskedStatus> maskedStatus() async => MaskedStatus.fromJson(await _api.getMap('/games/masked/status'));
  Future<void> maskedLeave() => _api.post('/games/masked/leave');
  Future<MaskedRoomState> room(String id) async => MaskedRoomState.fromJson(await _api.getMap('/games/masked/rooms/$id'));
  Future<MaskedRoomState> maskedFinish(String id, {String text = '', bool skip = false}) async =>
      MaskedRoomState.fromJson(await _api.postMap('/games/masked/rooms/$id/finish', body: {'text': text, 'skip': skip}));
  Future<void> report(String room, String answer) => _api.post('/games/masked/rooms/$room/answers/$answer/report', body: {'reason': 'inappropriate_content'});
}

final partyRepositoryProvider = Provider<PartyRepository>((ref) => PartyRepository(ref.watch(apiClientProvider)));

/// The turn controls shared by both group games: who is up, choose truth or
/// dare, the prompt, then done or skip.
class PartyControls extends StatelessWidget {
  const PartyControls({super.key, required this.state, required this.busy, required this.onPick, required this.onDone, required this.onSkip, required this.onTakeOver, this.answerField});
  final PartyState state;
  final bool busy;
  final void Function(String kind) onPick;
  final VoidCallback onDone;
  final VoidCallback onSkip;
  final VoidCallback onTakeOver;

  /// For masked rooms: where the player writes their answer.
  final Widget? answerField;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Wrap(spacing: 8, runSpacing: 4, children: [
        for (final p in state.players)
          Chip(
            key: Key('player_${p.label}'),
            avatar: p.turn ? const Icon(Icons.play_arrow, size: 16) : null,
            label: Text(p.you ? '${p.label} (you)' : p.label),
            visualDensity: VisualDensity.compact,
          ),
      ]),
      const SizedBox(height: 16),
      if (state.phase == 'choose')
        state.yourTurn
            ? Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text('Your turn', key: const Key('party_turn'), style: t.textTheme.headlineSmall),
                const SizedBox(height: 12),
                Row(children: [
                  Expanded(child: FilledButton(key: const Key('pick_truth'), onPressed: busy ? null : () => onPick('truth'), child: Text(AppLocalizations.of(context)?.sTruth ?? 'Truth'))),
                  const SizedBox(width: 12),
                  Expanded(child: FilledButton.tonal(key: const Key('pick_dare'), onPressed: busy ? null : () => onPick('dare'), child: Text(AppLocalizations.of(context)?.sDare ?? 'Dare'))),
                ]),
              ])
            : Text('${state.turnLabel} is choosing...', key: const Key('party_waiting'), style: t.textTheme.titleMedium)
      else ...[
        Text(state.kind == 'truth' ? 'Truth' : 'Dare', style: t.textTheme.labelLarge),
        const SizedBox(height: 6),
        Text(state.prompt, key: const Key('party_prompt'), style: t.textTheme.headlineSmall),
        const SizedBox(height: 14),
        if (state.yourTurn) ...[
          ?answerField,
          const SizedBox(height: 8),
          Row(children: [
            Expanded(child: FilledButton(key: const Key('party_done'), onPressed: busy ? null : onDone, child: Text(state.kind == 'truth' ? 'Answer' : 'Done'))),
            const SizedBox(width: 12),
            OutlinedButton(key: const Key('party_skip'), onPressed: busy || state.skipsLeft == 0 ? null : onSkip, child: Text('Skip (${state.skipsLeft} left)')),
          ]),
        ] else
          Text('${state.turnLabel} is answering.', style: t.textTheme.bodyMedium),
      ],
      if (state.canTakeOver) ...[
        const SizedBox(height: 12),
        TextButton(key: const Key('party_takeover'), onPressed: busy ? null : onTakeOver, child: const Text('They are away: move on')),
      ],
    ]);
  }
}

/// Truth or Dare for a matched group chat. The answer is spoken or typed in
/// the chat (encrypted); this screen only keeps the turns and the prompts.
class GroupTruthOrDareScreen extends ConsumerStatefulWidget {
  const GroupTruthOrDareScreen({super.key, required this.groupMatchId, this.pollInterval = const Duration(seconds: 2)});
  final String groupMatchId;
  final Duration pollInterval;

  @override
  ConsumerState<GroupTruthOrDareScreen> createState() => _GroupTruthOrDareState();
}

class _GroupTruthOrDareState extends ConsumerState<GroupTruthOrDareScreen> {
  late final PartyRepository _repo;
  PartyState? _state;
  Object? _error;
  LiveRefresh? _live;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(partyRepositoryProvider);
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

  Future<void> _act(Future<PartyState> Function() f) async {
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
    final s = _state;
    final id = widget.groupMatchId;
    Widget body;
    if (_error != null) {
      body = ErrorView(error: _error!, onRetry: _load);
    } else if (s == null) {
      body = const Loader();
    } else if (!s.active) {
      body = EmptyState(
        icon: Icons.casino_outlined,
        title: 'Truth or Dare',
        message: 'Everyone takes a turn in order. Pick truth or dare, answer in the chat, then pass it on. Two skips each.',
        action: FilledButton(key: const Key('party_start'), onPressed: _busy ? null : () => _act(() => _repo.start(id)), child: Text(AppLocalizations.of(context)?.sStartTheGame ?? 'Start the game')),
      );
    } else {
      body = ListView(padding: const EdgeInsets.all(20), children: [
        PartyControls(
          state: s,
          busy: _busy,
          onPick: (k) => _act(() => _repo.pick(id, k)),
          onDone: () => _act(() => _repo.done(id)),
          onSkip: () => _act(() => _repo.skip(id)),
          onTakeOver: () => _act(() => _repo.takeOver(id)),
        ),
      ]);
    }
    return Scaffold(appBar: AppBar(title: Text(AppLocalizations.of(context)?.sTruthOrDare ?? 'Truth or Dare')), body: body);
  }
}

/// Masked Truth in a room of strangers under made-up names. Join the queue,
/// play for about 25 minutes. There is no chat: a truth is answered in a short
/// screened text that everyone in the room sees. Not end-to-end encrypted, and
/// the screen says so.
class MaskedRoomScreen extends ConsumerStatefulWidget {
  const MaskedRoomScreen({super.key, this.pollInterval = const Duration(seconds: 2)});
  final Duration pollInterval;

  @override
  ConsumerState<MaskedRoomScreen> createState() => _MaskedRoomState();
}

class _MaskedRoomState extends ConsumerState<MaskedRoomScreen> {
  late final PartyRepository _repo;
  MaskedStatus _status = MaskedStatus(state: 'idle');
  MaskedRoomState? _room;
  Object? _error;
  Timer? _timer;
  LiveRefresh? _roomLive;
  bool _busy = false;
  final _text = TextEditingController();

  @override
  void initState() {
    super.initState();
    _repo = ref.read(partyRepositoryProvider);
    _tick();
    _timer = Timer.periodic(widget.pollInterval, (_) {
      if (_roomLive == null) _tick();
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    _roomLive?.dispose();
    _text.dispose();
    super.dispose();
  }

  Future<void> _tick() async {
    if (_busy || !mounted) return;
    try {
      if (_status.state == 'in_room' && _status.roomId.isNotEmpty) {
        final r = await _repo.room(_status.roomId);
        if (mounted) setState(() => _room = r);
        if (mounted) {
          _roomLive ??= LiveRefresh.of(ref, kind: 'masked', id: _status.roomId, interval: widget.pollInterval, onTick: _tick).start();
        }
      } else if (_status.state == 'waiting') {
        final s = await _repo.maskedStatus();
        if (mounted) setState(() => _status = s);
      }
    } on ApiException catch (e) {
      if (e.status == 404 && mounted) {
        setState(() {
          _room = null;
          _status = MaskedStatus(state: 'idle');
        });
      }
    } catch (_) {}
  }

  Future<void> _join() async {
    setState(() => _busy = true);
    try {
      final s = await _repo.maskedJoin();
      if (mounted) setState(() => _status = s);
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _leave() async {
    await guarded(context, _repo.maskedLeave);
    if (mounted) {
      setState(() {
        _status = MaskedStatus(state: 'idle');
        _room = null;
      });
    }
  }

  Future<void> _gameAct(Future<PartyState> Function() f) async {
    setState(() => _busy = true);
    try {
      await f();
      final r = await _repo.room(_status.roomId);
      if (mounted) setState(() => _room = r);
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _finish({bool skip = false}) async {
    setState(() => _busy = true);
    try {
      final r = await _repo.maskedFinish(_status.roomId, text: _text.text.trim(), skip: skip);
      _text.clear();
      if (mounted) setState(() => _room = r);
    } catch (e) {
      if (mounted) showError(context, e);
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    Widget body;
    final room = _room;
    if (_error != null) {
      body = ErrorView(error: _error!, onRetry: _tick);
    } else if (_status.state == 'idle') {
      body = EmptyState(
        icon: Icons.theater_comedy_outlined,
        title: 'Masked Truth',
        message: 'Join three to five strangers for about 25 minutes. Everyone gets a made-up name. Answers are short texts the whole room can see; they are screened but not end-to-end encrypted, and the room disappears afterwards.',
        action: FilledButton(key: const Key('masked_join'), onPressed: _busy ? null : _join, child: Text(AppLocalizations.of(context)?.sFindARoom ?? 'Find a room')),
      );
    } else if (_status.state == 'waiting' || room == null) {
      body = Center(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          const CircularProgressIndicator(),
          const SizedBox(height: 16),
          Text(_status.state == 'waiting' ? '${_status.waiting} waiting. Looking for a room...' : 'Joining...', key: const Key('masked_waiting'), style: t.textTheme.titleMedium),
          const SizedBox(height: 12),
          TextButton(key: const Key('masked_cancel'), onPressed: _leave, child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel')),
        ]),
      );
    } else {
      final g = room.game;
      final id = _status.roomId;
      body = ListView(padding: const EdgeInsets.all(20), children: [
        Text('You are ${room.myAlias}', key: const Key('masked_alias'), style: t.textTheme.titleMedium),
        const SizedBox(height: 4),
        Text('Not end-to-end encrypted. Be kind: you can report any answer.', style: t.textTheme.bodySmall),
        const SizedBox(height: 16),
        if (!g.active)
          FilledButton(key: const Key('party_start'), onPressed: _busy ? null : () => _gameAct(() => _repo.start(id)), child: Text(AppLocalizations.of(context)?.sStartTheGame ?? 'Start the game'))
        else
          PartyControls(
            state: g,
            busy: _busy,
            onPick: (k) => _gameAct(() => _repo.pick(id, k)),
            onDone: _finish,
            onSkip: () => _finish(skip: true),
            onTakeOver: () => _gameAct(() => _repo.takeOver(id)),
            answerField: g.kind == 'truth'
                ? TextField(key: const Key('masked_answer'), controller: _text, maxLength: 200, minLines: 1, maxLines: 3, decoration: const InputDecoration(hintText: 'Your answer'))
                : null,
          ),
        const SizedBox(height: 20),
        Text('What has been said', style: t.textTheme.titleMedium),
        const SizedBox(height: 6),
        if (room.answers.isEmpty) Text('Nothing yet.', style: t.textTheme.bodySmall),
        for (final a in room.answers)
          Card(
            key: Key('answer_${a.id}'),
            child: ListTile(
              title: Text(a.prompt, style: t.textTheme.bodySmall),
              subtitle: Text('${a.mine ? 'You' : a.alias}: ${a.body.isEmpty ? 'did the dare' : a.body}'),
              trailing: a.mine
                  ? null
                  : IconButton(
                      key: Key('report_${a.id}'),
                      tooltip: 'Report',
                      icon: const Icon(Icons.flag_outlined),
                      onPressed: () async {
                        final ok = await guarded(context, () => _repo.report(id, a.id));
                        if (ok && context.mounted) showMessage(context, 'Reported. Thank you.');
                      },
                    ),
            ),
          ),
        const SizedBox(height: 12),
        TextButton(key: const Key('masked_leave'), onPressed: _leave, child: Text(AppLocalizations.of(context)?.sLeaveTheRoom ?? 'Leave the room')),
      ]);
    }
    return Scaffold(appBar: AppBar(title: Text(AppLocalizations.of(context)?.sMaskedTruth ?? 'Masked Truth')), body: body);
  }
}
