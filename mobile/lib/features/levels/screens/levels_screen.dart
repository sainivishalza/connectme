import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../levels_repository.dart';

/// Your levels and who is on top. Charm grows with gifts you receive and is
/// shown on your profile by default; Supporter grows with gifts you send and
/// stays private unless you switch it on. Other people only ever see a level,
/// never a coin amount.
class LevelsScreen extends ConsumerWidget {
  const LevelsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return DefaultTabController(
      length: 2,
      child: Scaffold(
        appBar: AppBar(
          title: Text(AppLocalizations.of(context)?.sLevels ?? 'Levels'),
          bottom: const TabBar(isScrollable: true, tabAlignment: TabAlignment.start, tabs: [Tab(key: Key('tab_mine'), text: 'My levels'), Tab(key: Key('tab_boards'), text: 'Leaderboards')]),
        ),
        body: const TabBarView(children: [_Mine(), _Boards()]),
      ),
    );
  }
}

class _Mine extends ConsumerWidget {
  const _Mine();

  Future<void> _save(BuildContext context, WidgetRef ref, MyLevels m, {bool? charm, bool? supporter}) async {
    final ok = await guarded(context, () async {
      await ref.read(levelsRepositoryProvider).setVisibility(charm: charm ?? m.showCharm, supporter: supporter ?? m.showSupporter);
      ref.invalidate(myLevelsProvider);
      await ref.read(myLevelsProvider.future);
    });
    if (!ok && context.mounted) ref.invalidate(myLevelsProvider);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final v = ref.watch(myLevelsProvider);
    if (!v.hasValue) return v.hasError ? ErrorView(error: v.error!, onRetry: () => ref.invalidate(myLevelsProvider)) : const Loader();
    final m = v.requireValue;
    return ListView(padding: const EdgeInsets.all(16), children: [
      _LadderCard(
        keyName: 'charm_card',
        title: 'Charm',
        blurb: 'Grows with gifts you receive.',
        icon: Icons.auto_awesome,
        level: m.charm,
      ),
      const SizedBox(height: 12),
      _LadderCard(
        keyName: 'supporter_card',
        title: 'Supporter',
        blurb: 'Grows with gifts you send.',
        icon: Icons.volunteer_activism,
        level: m.supporter,
      ),
      const SizedBox(height: 8),
      SwitchListTile(
        key: const Key('show_charm'),
        title: const Text('Show my Charm level'),
        subtitle: const Text('A small badge on your profile and live rooms'),
        value: m.showCharm,
        onChanged: (x) => _save(context, ref, m, charm: x),
      ),
      SwitchListTile(
        key: const Key('show_supporter'),
        title: const Text('Show me on the Supporters board'),
        subtitle: const Text('Off by default. Only your level and gift count show, never an amount'),
        value: m.showSupporter,
        onChanged: (x) => _save(context, ref, m, supporter: x),
      ),
      const Divider(),
      ListTile(
        key: const Key('open_earnings'),
        contentPadding: EdgeInsets.zero,
        leading: const Icon(Icons.payments_outlined),
        title: const Text('Earnings and payouts'),
        subtitle: const Text('Coins from live gifts, and how to be paid'),
        trailing: const Icon(Icons.chevron_right),
        onTap: () => context.push('/profile/earnings'),
      ),
    ]);
  }
}

class _LadderCard extends StatelessWidget {
  const _LadderCard({required this.keyName, required this.title, required this.blurb, required this.icon, required this.level});
  final String keyName;
  final String title;
  final String blurb;
  final IconData icon;
  final LevelInfo level;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Card(
      key: Key(keyName),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Wrap(
            alignment: WrapAlignment.spaceBetween,
            crossAxisAlignment: WrapCrossAlignment.center,
            spacing: 12,
            runSpacing: 4,
            children: [
              Row(mainAxisSize: MainAxisSize.min, children: [
                Icon(icon, color: t.colorScheme.primary),
                const SizedBox(width: 8),
                Flexible(child: Text(title, style: t.textTheme.titleMedium)),
              ]),
              Text(level.label, key: Key('${keyName}_label'), style: t.textTheme.titleMedium?.copyWith(color: t.colorScheme.primary)),
            ],
          ),
          const SizedBox(height: 4),
          Text(blurb, style: t.textTheme.bodySmall),
          const SizedBox(height: 12),
          ClipRRect(
            borderRadius: BorderRadius.circular(6),
            child: LinearProgressIndicator(minHeight: 10, value: level.maxed ? 1 : level.progress / 100),
          ),
          const SizedBox(height: 6),
          Text(
            level.maxed ? 'Top level reached' : '${level.total} of ${level.nextAt} coins to Lv ${level.level + 1}',
            style: t.textTheme.bodySmall,
          ),
        ]),
      ),
    );
  }
}

class _Boards extends ConsumerStatefulWidget {
  const _Boards();

  @override
  ConsumerState<_Boards> createState() => _BoardsState();
}

class _BoardsState extends ConsumerState<_Boards> {
  String _kind = 'hosts';
  String _period = 'week';

  @override
  Widget build(BuildContext context) {
    final board = ref.watch(boardProvider((_kind, _period)));
    return Column(children: [
      Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
        child: Wrap(key: const Key('board_kind'), spacing: 8, runSpacing: 4, children: [
          for (final k in const [('hosts', 'Top hosts'), ('supporters', 'Top supporters')])
            ChoiceChip(label: Text(k.$2), selected: _kind == k.$1, onSelected: (_) => setState(() => _kind = k.$1)),
        ]),
      ),
      Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
        child: Wrap(key: const Key('board_period'), spacing: 8, runSpacing: 4, children: [
          for (final k in const [('week', 'Week'), ('month', 'Month'), ('all', 'All time')])
            ChoiceChip(label: Text(k.$2), selected: _period == k.$1, onSelected: (_) => setState(() => _period = k.$1)),
        ]),
      ),
      Expanded(
        child: board.when(
          loading: () => const Loader(),
          error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(boardProvider((_kind, _period)))),
          data: (b) => b.entries.isEmpty
              ? EmptyState(
                  icon: Icons.emoji_events_outlined,
                  title: 'Nobody here yet',
                  message: _kind == 'hosts' ? 'Go live and collect gifts to get on the board.' : 'Send gifts and switch on your Supporter level to appear.')
              : ListView(padding: const EdgeInsets.all(16), children: [
                  if (b.myRank > 0) Padding(padding: const EdgeInsets.only(bottom: 8), child: Text('You are #${b.myRank}', key: const Key('my_rank'))),
                  for (final e in b.entries)
                    Card(
                      color: e.me ? Theme.of(context).colorScheme.primaryContainer : null,
                      child: ListTile(
                        key: Key('rank_${e.rank}'),
                        leading: CircleAvatar(child: Text('${e.rank}')),
                        title: Text(e.name.isEmpty ? 'Member' : e.name, maxLines: 1, overflow: TextOverflow.ellipsis),
                        subtitle: Text('Lv ${e.level} ${e.title} - ${e.gifts} ${e.gifts == 1 ? 'gift' : 'gifts'}'),
                      ),
                    ),
                ]),
        ),
      ),
    ]);
  }
}
