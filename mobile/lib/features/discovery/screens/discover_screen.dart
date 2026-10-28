import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';
import '../../../l10n/app_localizations.dart';
import '../../../core/network/api_client.dart';
import '../../../shared/widgets/app_button.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../payments/payments_repository.dart';
import '../../profile/profile_repository.dart';
import '../discovery_repository.dart';
import '../widgets/deck_actions.dart';
import '../widgets/deck_card.dart';
import '../widgets/filter_sheet.dart';
import '../widgets/match_overlay.dart';
import '../widgets/profile_card.dart';
import '../widgets/profile_sheet.dart';
import '../widgets/swipe_stack.dart';

class DiscoverScreen extends ConsumerWidget {
  const DiscoverScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return DefaultTabController(
      length: 4,
      child: Scaffold(
        appBar: AppBar(
          title: Text(AppLocalizations.of(context)?.discoverTitle ?? 'Discover'),
          actions: [
            IconButton(
              key: const Key('open_explore'),
              tooltip: 'Explore',
              icon: const Icon(Icons.explore_outlined),
              onPressed: () => context.push('/explore'),
            ),
            IconButton(
              tooltip: 'Filters',
              icon: Badge(
                isLabelVisible: ref.watch(nearbyFilterProvider).activeCount > 0,
                label: Text('${ref.watch(nearbyFilterProvider).activeCount}'),
                child: const Icon(Icons.tune),
              ),
              onPressed: () => showModalBottomSheet<void>(
                context: context, useRootNavigator: true,
                isScrollControlled: true,
                showDragHandle: true,
                builder: (_) => const FilterSheet(),
              ),
            ),
            IconButton(
              tooltip: 'Boost',
              icon: const Icon(Icons.bolt),
              onPressed: () => _boost(context, ref),
            ),
          ],
          bottom: TabBar(isScrollable: true, tabAlignment: TabAlignment.start, tabs: [
            Tab(text: AppLocalizations.of(context)?.tabSwipe ?? 'Swipe'),
            Tab(text: AppLocalizations.of(context)?.tabTopPicks ?? 'Top picks'),
            Tab(text: AppLocalizations.of(context)?.tabNearby ?? 'Nearby'),
            Tab(text: AppLocalizations.of(context)?.tabLikesYou ?? 'Likes you'),
          ]),
        ),
        body: const TabBarView(children: [SwipeDeck(), TopPicksList(), NearbyList(), LikesYouList()]),
      ),
    );
  }

  Future<void> _boost(BuildContext context, WidgetRef ref) async {
    final ok = await guarded(context, () => ref.read(discoveryRepositoryProvider).boost());
    if (!context.mounted) return;
    if (ok) {
      showMessage(context, 'Boosted for 30 minutes');
      ref.invalidate(accountProvider);
    }
  }
}

/// Handles the errors every discovery screen can hit (no location, unverified).
class _NeedsSetup extends ConsumerWidget {
  const _NeedsSetup({required this.error, required this.onRetry});
  final Object error;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final e = error;
    if (e is ApiException && e.status == 403) {
      final needsLocation = e.message.toLowerCase().contains('location');
      return EmptyState(
        icon: needsLocation ? Icons.location_off_outlined : Icons.mark_email_unread_outlined,
        title: needsLocation ? 'Share your location' : 'Verify your email',
        message: needsLocation
            ? 'We use it to find people near you. Others only see a rough distance.'
            : 'Verify your email to see and meet people.',
        action: needsLocation
            ? AppButton(
                key: const Key('share_location'),
                label: 'Share location',
                onPressed: () async {
                  if (await guarded(context, () => shareLocation(ref))) onRetry();
                },
              )
            : null,
      );
    }
    return ErrorView(error: e, onRetry: onRetry);
  }
}

class SwipeDeck extends ConsumerStatefulWidget {
  const SwipeDeck({super.key});
  @override
  ConsumerState<SwipeDeck> createState() => _SwipeDeckState();
}

class _SwipeDeckState extends ConsumerState<SwipeDeck> {
  final Set<String> _done = {};
  final List<PublicProfile> _passed = []; // most recent last, for rewind
  final _controller = SwipeStackController();
  String _pendingNote = ''; // sent with the next like, then cleared

  /// Sends the swipe. Returns true if the card should leave the deck.
  Future<bool> _send(PublicProfile p, String action) async {
    final repo = ref.read(discoveryRepositoryProvider);
    try {
      final note = action == 'pass' ? '' : _pendingNote;
      _pendingNote = '';
      final r = action == 'superlike' ? await repo.superLike(p.ref, comment: note) : await repo.swipe(p.ref, action, comment: note);
      if (!mounted) return true;
      setState(() {
        _done.add(p.ref);
        if (action == 'pass') {
          _passed.add(p);
          if (_passed.length > 10) _passed.removeAt(0);
        } else {
          _passed.clear(); // rewind only undoes a pass that nothing has followed
        }
      });
      if (r.matched) _showMatch(r.matchId, p);
      return true;
    } on ApiException catch (e) {
      if (!mounted) return false;
      if (e.isPaymentRequired) {
        _offerUpgrade(e.message);
      } else {
        showMessage(context, e.message);
      }
      return false;
    }
  }

  Future<void> _rewind() async {
    if (_passed.isEmpty) {
      showMessage(context, 'Nothing to rewind');
      return;
    }
    try {
      await ref.read(discoveryRepositoryProvider).rewind();
      if (!mounted) return;
      setState(() => _done.remove(_passed.removeLast().ref));
    } on ApiException catch (e) {
      if (!mounted) return;
      if (e.isPaymentRequired) {
        _offerUpgrade(e.message);
      } else {
        showMessage(context, e.message);
      }
    }
  }

  void _showMatch(String? matchId, PublicProfile p) {
    showMatchOverlay(
      context,
      profile: p,
      onSayHi: () {
        if (matchId != null) context.go('/chats/$matchId');
      },
    );
  }

  void _offerUpgrade(String message) {
    showDialog<void>(
      context: context,
      builder: (c) => AlertDialog(
        title: Text(AppLocalizations.of(context)?.sGetMore ?? 'Get more'),
        content: Text(message),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c), child: Text(AppLocalizations.of(context)?.sNotNow ?? 'Not now')),
          FilledButton(
            onPressed: () {
              Navigator.pop(c);
              context.push('/profile/premium');
            },
            child: const Text('See plans'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final nearby = ref.watch(nearbyProvider);
    return nearby.when(
      loading: () => const Loader(),
      error: (e, _) => _NeedsSetup(error: e, onRetry: () => ref.invalidate(nearbyProvider)),
      data: (all) {
        final deck = all.where((p) => !_done.contains(p.ref)).toList();
        if (deck.isEmpty) {
          return EmptyState(
            icon: Icons.explore_outlined,
            title: "You're all caught up",
            message: 'No one new nearby right now. Widen your filters or check back soon.',
            action: OutlinedButton.icon(
              onPressed: () {
                _done.clear();
                _passed.clear();
                ref.invalidate(nearbyProvider);
              },
              icon: const Icon(Icons.refresh),
              label: Text(AppLocalizations.of(context)?.sRefresh ?? 'Refresh'),
            ),
          );
        }
        return Column(children: [
          Expanded(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(14, 8, 14, 6),
              child: SwipeStack<PublicProfile>(
                controller: _controller,
                items: deck,
                keyOf: (p) => p.ref,
                onSwipe: _send,
                labelOf: describeProfile,
                cardBuilder: (context, p, progress) => DeckCard(
                  profile: p,
                  like: progress.like,
                  nope: progress.nope,
                  superLike: progress.superLike,
                  onOpen: () => showProfileSheet(
                    context,
                    p,
                    actions: LikeWithNote(onSend: (note) {
                      Navigator.of(context).pop();
                      _pendingNote = note;
                      _controller.swipe('like');
                    }),
                  ),
                ),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 6),
            child: DeckActions(
              onRewind: _rewind,
              onPass: () => _controller.swipe('pass'),
              onSuperLike: () => _controller.swipe('superlike'),
              onLike: () => _controller.swipe('like'),
            ),
          ),
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Text(deck.length > 1 ? '${deck.length - 1} more nearby' : 'Last one nearby', style: Theme.of(context).textTheme.bodySmall),
          ),
        ]);
      },
    );
  }
}

/// Today's best people for you, each with a sentence on why.
class TopPicksList extends ConsumerStatefulWidget {
  const TopPicksList({super.key});
  @override
  ConsumerState<TopPicksList> createState() => _TopPicksListState();
}

class _TopPicksListState extends ConsumerState<TopPicksList> {
  final Set<String> _done = {};

  Future<void> _act(TopPick p, String action) async {
    try {
      final r = await ref.read(discoveryRepositoryProvider).swipe(p.profile.ref, action);
      if (!mounted) return;
      setState(() => _done.add(p.profile.ref));
      if (r.matched) {
        showMatchOverlay(context, profile: p.profile, onSayHi: () {
          if (r.matchId != null) context.go('/chats/${r.matchId}');
        });
      }
    } on ApiException catch (e) {
      if (mounted) showMessage(context, e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final picks = ref.watch(topPicksProvider);
    if (picks.hasError && !picks.hasValue) {
      return _NeedsSetup(error: picks.error!, onRetry: () => ref.invalidate(topPicksProvider));
    }
    return AsyncListView<TopPick>(
      value: picks.whenData((l) => l.where((p) => !_done.contains(p.profile.ref)).toList()),
      onRefresh: () async => ref.refresh(topPicksProvider.future),
      emptyTitle: 'No picks right now',
      emptyMessage: 'Check back later, or widen your filters and share more about yourself so we can find people you will click with.',
      emptyIcon: Icons.auto_awesome_outlined,
      itemBuilder: (_, p) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(4, 0, 4, 8),
          child: Row(children: [
            const Icon(Icons.auto_awesome, size: 18),
            const SizedBox(width: 8),
            Expanded(child: Text(p.why, key: const Key('pick_why'), style: Theme.of(context).textTheme.titleSmall)),
          ]),
        ),
        ProfileCard(profile: p.profile, onPass: () => _act(p, 'pass'), onLike: () => _act(p, 'like')),
      ]),
    );
  }
}

class NearbyList extends ConsumerWidget {
  const NearbyList({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final nearby = ref.watch(nearbyProvider);
    if (nearby.hasError && !nearby.hasValue) {
      return _NeedsSetup(error: nearby.error!, onRetry: () => ref.invalidate(nearbyProvider));
    }
    return AsyncListView<PublicProfile>(
      value: nearby,
      onRefresh: () async => ref.refresh(nearbyProvider.future),
      emptyTitle: 'No one nearby',
      emptyMessage: 'Try a wider radius in the filters.',
      emptyIcon: Icons.people_outline,
      itemBuilder: (_, p) => ProfileCard(profile: p),
    );
  }
}

class LikesYouList extends ConsumerWidget {
  const LikesYouList({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final likes = ref.watch(likesYouProvider);
    return likes.when(
      loading: () => const Loader(),
      error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(likesYouProvider)),
      data: (l) {
        if (l.locked) {
          return EmptyState(
            icon: Icons.favorite_border,
            title: l.count == 0 ? 'No likes yet' : '${l.count} ${l.count == 1 ? 'person likes' : 'people like'} you',
            message: l.count == 0 ? 'Keep swiping.' : 'Upgrade to Gold to see who.',
            action: l.count == 0
                ? null
                : AppButton(label: 'See plans', onPressed: () => context.push('/profile/premium')),
          );
        }
        return AsyncListView<PublicProfile>(
          value: AsyncValue.data(l.profiles),
          onRefresh: () async => ref.refresh(likesYouProvider.future),
          emptyTitle: 'No likes yet',
          emptyIcon: Icons.favorite_border,
          itemBuilder: (_, p) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            if ((l.comments[p.ref] ?? '').isNotEmpty) _LikeNote(text: l.comments[p.ref]!),
            ProfileCard(profile: p),
          ]),
        );
      },
    );
  }
}

/// The note someone sent with their like, shown above their card.
class _LikeNote extends StatelessWidget {
  const _LikeNote({required this.text});
  final String text;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Container(
      key: const Key('like_note_bubble'),
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(color: t.colorScheme.primaryContainer.withValues(alpha: 0.5), borderRadius: BorderRadius.circular(16)),
      child: Row(children: [
        Icon(Icons.format_quote, color: t.colorScheme.primary),
        const SizedBox(width: 8),
        Expanded(child: Text(text, style: t.textTheme.bodyLarge)),
      ]),
    );
  }
}
