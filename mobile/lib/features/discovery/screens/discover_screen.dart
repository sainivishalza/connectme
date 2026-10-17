import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';
import '../../../core/network/api_client.dart';
import '../../../shared/widgets/app_button.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../payments/payments_repository.dart';
import '../../profile/profile_repository.dart';
import '../discovery_repository.dart';
import '../widgets/filter_sheet.dart';
import '../widgets/profile_card.dart';

class DiscoverScreen extends ConsumerWidget {
  const DiscoverScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return DefaultTabController(
      length: 3,
      child: Scaffold(
        appBar: AppBar(
          title: const Text('Discover'),
          actions: [
            IconButton(
              key: const Key('open_explore'),
              tooltip: 'Explore',
              icon: const Icon(Icons.explore_outlined),
              onPressed: () => context.push('/explore'),
            ),
            IconButton(
              tooltip: 'Filters',
              icon: const Icon(Icons.tune),
              onPressed: () => showModalBottomSheet<void>(
                context: context,
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
          bottom: const TabBar(tabs: [Tab(text: 'Swipe'), Tab(text: 'Nearby'), Tab(text: 'Likes you')]),
        ),
        body: const TabBarView(children: [SwipeDeck(), NearbyList(), LikesYouList()]),
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
  bool _busy = false;

  Future<void> _act(PublicProfile p, String action) async {
    if (_busy) return;
    setState(() => _busy = true);
    final repo = ref.read(discoveryRepositoryProvider);
    try {
      final r = action == 'superlike' ? await repo.superLike(p.ref) : await repo.swipe(p.ref, action);
      if (!mounted) return;
      setState(() => _done.add(p.ref));
      if (r.matched) _showMatch(r.matchId);
    } on ApiException catch (e) {
      if (!mounted) return;
      if (e.isPaymentRequired) {
        _offerUpgrade(e.message);
      } else {
        showMessage(context, e.message);
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _showMatch(String? matchId) {
    showDialog<void>(
      context: context,
      builder: (c) => AlertDialog(
        icon: const Icon(Icons.favorite, size: 40),
        title: const Text("It's a match!"),
        content: const Text('Say hello. You can reveal yourselves to each other whenever you both want.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c), child: const Text('Keep swiping')),
          FilledButton(
            onPressed: () {
              Navigator.pop(c);
              if (matchId != null) context.go('/chats/$matchId');
            },
            child: const Text('Say hi'),
          ),
        ],
      ),
    );
  }

  void _offerUpgrade(String message) {
    showDialog<void>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Get more'),
        content: Text(message),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c), child: const Text('Not now')),
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
                ref.invalidate(nearbyProvider);
              },
              icon: const Icon(Icons.refresh),
              label: const Text('Refresh'),
            ),
          );
        }
        final p = deck.first;
        return RefreshIndicator(
          onRefresh: () async => ref.refresh(nearbyProvider.future),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              ProfileCard(
                key: ValueKey(p.ref),
                profile: p,
                onPass: _busy ? null : () => _act(p, 'pass'),
                onLike: _busy ? null : () => _act(p, 'like'),
                onSuperLike: _busy ? null : () => _act(p, 'superlike'),
              ),
              const SizedBox(height: 8),
              Text('${deck.length - 1} more nearby', textAlign: TextAlign.center, style: Theme.of(context).textTheme.bodySmall),
            ],
          ),
        );
      },
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
          itemBuilder: (_, p) => ProfileCard(profile: p),
        );
      },
    );
  }
}
