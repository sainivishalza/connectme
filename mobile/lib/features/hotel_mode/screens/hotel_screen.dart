import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import '../../../core/theme/tokens.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_button.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../discovery/discovery_repository.dart';
import '../../discovery/widgets/profile_card.dart';
import '../../profile/profile_repository.dart';
import '../hotel_repository.dart';
import '../moods.dart';

/// Hotel / same-place discovery. Not checked in: detect a venue and check in.
/// Checked in: anonymous feed, "anyone want dinner?" board and drinks.
class HotelScreen extends ConsumerWidget {
  const HotelScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final stay = ref.watch(currentStayProvider);
    return stay.when(
      loading: () => const Scaffold(body: Loader()),
      error: (e, _) => Scaffold(
        appBar: AppBar(title: Text(AppLocalizations.of(context)?.sHotelMode ?? 'Hotel mode')),
        body: ErrorView(error: e, onRetry: () => ref.invalidate(currentStayProvider)),
      ),
      data: (s) => s == null ? const _CheckInView() : _StayView(stay: s),
    );
  }
}

class _CheckInView extends ConsumerStatefulWidget {
  const _CheckInView();
  @override
  ConsumerState<_CheckInView> createState() => _CheckInViewState();
}

class _CheckInViewState extends ConsumerState<_CheckInView> {
  bool _locating = false;
  String? _busyHotel;

  Future<void> _locate() async {
    setState(() => _locating = true);
    final ok = await guarded(context, () => shareLocation(ref));
    if (mounted) setState(() => _locating = false);
    if (ok) ref.invalidate(detectedHotelsProvider);
  }

  Future<void> _checkIn(Hotel h) async {
    final yes = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: Text('Check in at ${h.name}?'),
        content: const Text(
          'You will be anonymous to other guests until you both choose to reveal. '
          'Everything from your stay (chats, matches, gifts, posts) is deleted 24 hours after you check out.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: Text(AppLocalizations.of(context)?.sNotNow ?? 'Not now')),
          FilledButton(onPressed: () => Navigator.pop(c, true), child: Text(AppLocalizations.of(context)?.sCheckIn ?? 'Check in')),
        ],
      ),
    );
    if (yes != true || !mounted) return;
    setState(() => _busyHotel = h.id);
    final ok = await guarded(context, () async {
      // The server verifies a fresh location inside the fence, so refresh it first.
      await shareLocation(ref);
      await ref.read(hotelRepositoryProvider).checkIn(h.id);
    });
    if (mounted) setState(() => _busyHotel = null);
    if (ok) {
      ref.invalidate(currentStayProvider);
      ref.invalidate(meProvider);
    }
  }

  @override
  Widget build(BuildContext context) {
    final hotels = ref.watch(detectedHotelsProvider);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.sHotelMode ?? 'Hotel mode')),
      body: hotels.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(detectedHotelsProvider)),
        data: (list) => RefreshIndicator(
          onRefresh: () async => ref.refresh(detectedHotelsProvider.future),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              const _HotelIntro(),
              const SizedBox(height: 12),
              AppButton(
                key: const Key('detect'),
                label: 'Find my venue',
                icon: Icons.my_location,
                busy: _locating,
                onPressed: _locate,
                outlined: true,
              ),
              const SizedBox(height: 16),
              if (list.isEmpty)
                const EmptyState(
                  icon: Icons.location_searching,
                  title: 'No venue detected',
                  message: 'Tap "Find my venue" when you are at a hotel, resort, festival or conference.',
                )
              else
                for (final h in list)
                  Card(
                    child: ListTile(
                      leading: const Icon(Icons.apartment),
                      title: Text(h.name),
                      trailing: FilledButton(
                        key: Key('checkin_${h.id}'),
                        onPressed: _busyHotel == null ? () => _checkIn(h) : null,
                        style: FilledButton.styleFrom(minimumSize: const Size(90, 40)),
                        child: _busyHotel == h.id
                            ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))
                            : Text(AppLocalizations.of(context)?.sCheckIn ?? 'Check in'),
                      ),
                    ),
                  ),
            ],
          ),
        ),
      ),
    );
  }
}

class _StayView extends ConsumerWidget {
  const _StayView({required this.stay});
  final CheckIn stay;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final id = stay.hotel.id;
    return DefaultTabController(
      length: 3,
      child: Scaffold(
        appBar: AppBar(
          title: Text(stay.hotel.name),
          actions: [
            TextButton(
              key: const Key('checkout'),
              onPressed: () async {
                final ok = await guarded(context, () => ref.read(hotelRepositoryProvider).checkOut());
                if (ok) {
                  ref.invalidate(currentStayProvider);
                  ref.invalidate(meProvider);
                  if (context.mounted) showMessage(context, 'Checked out. Your stay data is deleted in 24 hours.');
                }
              },
              child: Text(AppLocalizations.of(context)?.sCheckOut ?? 'Check out'),
            ),
          ],
          bottom: const TabBar(tabs: [Tab(text: 'Guests'), Tab(text: 'Board'), Tab(text: 'Drinks')]),
        ),
        body: TabBarView(children: [_Guests(hotelId: id, myMood: stay.mood), _Board(hotelId: id), _Drinks(hotelId: id)]),
      ),
    );
  }
}

class _Guests extends ConsumerWidget {
  const _Guests({required this.hotelId, required this.myMood});
  final String hotelId;
  final String myMood;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final feed = ref.watch(hotelFeedProvider(hotelId));
    return Column(children: [
      _MoodBar(myMood: myMood),
      Expanded(child: _list(context, ref, feed)),
    ]);
  }

  Widget _list(BuildContext context, WidgetRef ref, AsyncValue<List<PublicProfile>> feed) {
    return AsyncListView<PublicProfile>(
      value: feed,
      onRefresh: () async => ref.refresh(hotelFeedProvider(hotelId).future),
      emptyTitle: 'No other guests yet',
      emptyMessage: 'People who check in here will appear, anonymously.',
      emptyIcon: Icons.people_outline,
      itemBuilder: (c, p) => ProfileCard(
        profile: p,
        onPass: () async {
          await guarded(c, () => ref.read(discoveryRepositoryProvider).swipe(p.ref, 'pass'));
          ref.invalidate(hotelFeedProvider(hotelId));
        },
        onLike: () async {
          SwipeResult? r;
          await guarded(c, () async => r = await ref.read(discoveryRepositoryProvider).swipe(p.ref, 'like'));
          ref.invalidate(hotelFeedProvider(hotelId));
          if (r?.matched == true && c.mounted) showMessage(c, "It's a match! Find them under Chats.");
        },
        onTap: () async {
          final ok = await guarded(c, () => ref.read(hotelRepositoryProvider).sendDrink(hotelId, p.ref));
          if (ok && c.mounted) showMessage(c, 'Drink sent anonymously 🍹');
        },
      ),
    );
  }
}

class _Board extends ConsumerStatefulWidget {
  const _Board({required this.hotelId});
  final String hotelId;
  @override
  ConsumerState<_Board> createState() => _BoardState();
}

class _BoardState extends ConsumerState<_Board> {
  final _c = TextEditingController();

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  Future<void> _post() async {
    final t = _c.text.trim();
    if (t.isEmpty) return;
    _c.clear();
    final ok = await guarded(context, () => ref.read(hotelRepositoryProvider).post(widget.hotelId, t));
    if (ok) ref.invalidate(hotelBoardProvider(widget.hotelId));
  }

  @override
  Widget build(BuildContext context) {
    final board = ref.watch(hotelBoardProvider(widget.hotelId));
    return Column(
      children: [
        Expanded(
          child: AsyncListView<BoardPost>(
            value: board,
            onRefresh: () async => ref.refresh(hotelBoardProvider(widget.hotelId).future),
            emptyTitle: 'The board is quiet',
            emptyMessage: 'Ask "Anyone want dinner?"',
            emptyIcon: Icons.forum_outlined,
            itemBuilder: (_, p) => Card(
              child: ListTile(
                leading: const Icon(Icons.theater_comedy_outlined),
                title: Text(p.message),
                subtitle: Text('Anonymous guest at ${p.createdAt.hour.toString().padLeft(2, '0')}:${p.createdAt.minute.toString().padLeft(2, '0')}'),
              ),
            ),
          ),
        ),
        SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(12, 4, 12, 8),
            child: Row(children: [
              Expanded(
                child: TextField(
                  key: const Key('board_input'),
                  controller: _c,
                  maxLength: 280,
                  decoration: const InputDecoration(hintText: 'Anyone want dinner?', counterText: ''),
                  onSubmitted: (_) => _post(),
                ),
              ),
              IconButton.filled(key: const Key('board_send'), onPressed: _post, icon: const Icon(Icons.send)),
            ]),
          ),
        ),
      ],
    );
  }
}

class _Drinks extends ConsumerWidget {
  const _Drinks({required this.hotelId});
  final String hotelId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final drinks = ref.watch(hotelDrinksProvider(hotelId));
    return AsyncListView<String>(
      value: drinks,
      onRefresh: () async => ref.refresh(hotelDrinksProvider(hotelId).future),
      emptyTitle: 'No drinks yet',
      emptyMessage: 'Tap a guest in the Guests tab to buy them a drink.',
      emptyIcon: Icons.local_bar_outlined,
      itemBuilder: (_, from) => const Card(
        child: ListTile(leading: Icon(Icons.local_bar), title: Text('Someone bought you a drink'), subtitle: Text('Anonymous guest')),
      ),
    );
  }
}

/// Hotel mode's own light (Lamplight): the one place this screen uses colour.
class _HotelIntro extends StatelessWidget {
  const _HotelIntro();

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    const light = ModeLight.lamplight;
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        gradient: light.wash(t.brightness),
        borderRadius: BorderRadius.circular(Radii.hero),
        border: Border.all(color: light.solid.withValues(alpha: 0.3)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 44,
            height: 44,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              gradient: light.gradient,
              boxShadow: [BoxShadow(color: light.solid.withValues(alpha: 0.4), blurRadius: 16, offset: const Offset(0, 6))],
            ),
            child: Icon(Icons.hotel_outlined, color: light.ink, size: 22),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('Meet people staying where you are', style: t.textTheme.titleMedium),
                const SizedBox(height: 6),
                Text('Anonymous by default. Your stay data auto-deletes 24h after checkout.', style: t.textTheme.bodyMedium),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Say what kind of traveller you are tonight, and filter the guest list by it.
class _MoodBar extends ConsumerStatefulWidget {
  const _MoodBar({required this.myMood});
  final String myMood;

  @override
  ConsumerState<_MoodBar> createState() => _MoodBarState();
}

class _MoodBarState extends ConsumerState<_MoodBar> {
  late String _mine = widget.myMood;

  Future<void> _pick(String mood) async {
    final next = _mine == mood ? '' : mood;
    if (await guarded(context, () => ref.read(hotelRepositoryProvider).setMood(next)) && mounted) {
      setState(() => _mine = next);
      ref.invalidate(currentStayProvider);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final filter = ref.watch(hotelMoodFilterProvider);
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('I am here as', style: t.textTheme.labelLarge),
        const SizedBox(height: 6),
        Wrap(spacing: 8, runSpacing: 4, children: [
          for (final e in travelMoods.entries)
            ChoiceChip(key: Key('my_mood_${e.key}'), label: Text(e.value), selected: _mine == e.key, onSelected: (_) => _pick(e.key)),
        ]),
        const SizedBox(height: 10),
        Text('Show guests who are', style: t.textTheme.labelLarge),
        const SizedBox(height: 6),
        Wrap(spacing: 8, runSpacing: 4, children: [
          ChoiceChip(key: const Key('filter_all'), label: Text(AppLocalizations.of(context)?.sEveryone ?? 'Everyone'), selected: filter.isEmpty, onSelected: (_) => ref.read(hotelMoodFilterProvider.notifier).set('')),
          for (final e in travelMoods.entries)
            ChoiceChip(
              key: Key('filter_${e.key}'),
              label: Text(e.value),
              selected: filter == e.key,
              onSelected: (_) => ref.read(hotelMoodFilterProvider.notifier).set(filter == e.key ? '' : e.key),
            ),
        ]),
      ]),
    );
  }
}
