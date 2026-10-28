import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../payments/payments_repository.dart' show urlOpenerProvider;
import '../partners_repository.dart';

/// Hotel mixers, concerts and hangouts, partner places you can book, and your
/// bookings.
class EventsScreen extends ConsumerWidget {
  const EventsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return DefaultTabController(
      length: 3,
      child: Scaffold(
        appBar: AppBar(
          title: const Text('Events and places'),
          bottom: const TabBar(tabs: [Tab(text: 'Events'), Tab(text: 'Places'), Tab(text: 'My bookings')]),
        ),
        body: const TabBarView(children: [_Events(), _Places(), _Bookings()]),
      ),
    );
  }
}

String _when(DateTime d) => DateFormat('EEE d MMM, HH:mm').format(d);

class _Events extends ConsumerWidget {
  const _Events();

  Future<void> _toggle(BuildContext context, WidgetRef ref, PartnerEvent e) async {
    final repo = ref.read(partnersRepositoryProvider);
    final ok = await guarded(context, () => e.joined ? repo.leave(e.id) : repo.join(e.id));
    if (!ok) return;
    ref.invalidate(partnerEventsProvider);
    if (context.mounted) {
      showMessage(context, e.joined ? (e.priceCoins > 0 ? 'You left. Your coins are refunded.' : 'You left.') : 'You are going.');
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = Theme.of(context);
    return AsyncListView<PartnerEvent>(
      value: ref.watch(partnerEventsProvider),
      onRefresh: () async => ref.refresh(partnerEventsProvider.future),
      emptyTitle: 'No events nearby',
      emptyMessage: 'Hotel mixers show up while you are checked in. Concerts and hangouts appear when partners schedule them near you.',
      emptyIcon: Icons.celebration_outlined,
      itemBuilder: (c, e) => Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Row(children: [
              Expanded(child: Text(e.title, style: t.textTheme.titleMedium)),
              Chip(label: Text(switch (e.kind) { 'mixer' => 'Mixer', 'concert' => 'Concert', _ => 'Hangout' }), visualDensity: VisualDensity.compact),
            ]),
            const SizedBox(height: 2),
            Text('${e.partner} · ${_when(e.startsAt)}${e.distance.isNotEmpty ? ' · ${e.distance}' : ''}', style: t.textTheme.bodySmall),
            if (e.description.isNotEmpty) ...[const SizedBox(height: 8), Text(e.description, style: t.textTheme.bodyMedium)],
            const SizedBox(height: 10),
            Row(children: [
              Expanded(
                child: Text(
                  '${e.going} going${e.capacity != null ? ' of ${e.capacity}' : ''} · ${e.priceCoins == 0 ? 'Free' : '${e.priceCoins} coins'}${e.guestsOnly ? ' · hotel guests' : ''}',
                  style: t.textTheme.bodySmall,
                ),
              ),
              if (e.joined) TextButton(key: Key('group_${e.id}'), onPressed: () => context.push('/eventgroup/${e.id}'), child: Text(AppLocalizations.of(context)?.sGroup ?? 'Group')),
              e.joined
                  ? OutlinedButton(key: Key('leave_${e.id}'), onPressed: () => _toggle(context, ref, e), child: Text(AppLocalizations.of(context)?.sLeave ?? 'Leave'))
                  : FilledButton(key: Key('join_${e.id}'), onPressed: e.full ? null : () => _toggle(context, ref, e), child: Text(e.full ? 'Full' : (e.priceCoins > 0 ? 'Join for ${e.priceCoins}' : 'Join'))),
            ]),
          ]),
        ),
      ),
    );
  }
}

class _Places extends ConsumerWidget {
  const _Places();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return AsyncListView<Partner>(
      value: ref.watch(partnerPlacesProvider),
      onRefresh: () async => ref.refresh(partnerPlacesProvider.future),
      emptyTitle: 'No partner places yet',
      emptyMessage: 'Share your location to see hotels, restaurants and activities near you.',
      emptyIcon: Icons.storefront_outlined,
      itemBuilder: (c, p) => Card(
        child: ListTile(
          key: Key('place_${p.id}'),
          leading: CircleAvatar(child: Icon(switch (p.kind) { 'hotel' => Icons.hotel_outlined, 'restaurant' => Icons.restaurant_outlined, _ => Icons.hiking })),
          title: Text(p.name),
          subtitle: Text([if (p.distance.isNotEmpty) p.distance, if (p.address.isNotEmpty) p.address].join(' · ')),
          trailing: FilledButton.tonal(key: Key('book_${p.id}'), onPressed: () => _book(c, ref, p), child: Text(AppLocalizations.of(context)?.sBook ?? 'Book')),
        ),
      ),
    );
  }

  Future<void> _book(BuildContext context, WidgetRef ref, Partner p) async {
    final made = await showModalBottomSheet<Booking>(
      context: context, useRootNavigator: true,
      isScrollControlled: true,
      builder: (_) => _BookSheet(partner: p, repo: ref.read(partnersRepositoryProvider)),
    );
    if (made != null) {
      ref.invalidate(myBookingsProvider);
      if (context.mounted) showMessage(context, 'Request sent. ${p.name} will confirm it.');
    }
  }
}

class _BookSheet extends StatefulWidget {
  const _BookSheet({required this.partner, required this.repo});
  final Partner partner;
  final PartnersRepository repo;

  @override
  State<_BookSheet> createState() => _BookSheetState();
}

class _BookSheetState extends State<_BookSheet> {
  int _party = 2;
  int _day = 1;
  int _hour = 19;
  final _note = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _note.dispose();
    super.dispose();
  }

  DateTime get _at {
    final n = DateTime.now().add(Duration(days: _day));
    return DateTime(n.year, n.month, n.day, _hour);
  }

  Future<void> _send() async {
    setState(() => _busy = true);
    try {
      final b = await widget.repo.book(widget.partner.id, partySize: _party, at: _at, note: _note.text.trim());
      if (mounted) Navigator.pop(context, b);
    } catch (e) {
      if (mounted) {
        showError(context, e);
        setState(() => _busy = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return SafeArea(
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(20, 4, 20, 20 + MediaQuery.viewInsetsOf(context).bottom),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Text('Book ${widget.partner.name}', style: t.textTheme.headlineSmall),
          const SizedBox(height: 4),
          Text('This is a request. The venue confirms it, and you pay them directly.', style: t.textTheme.bodyMedium),
          const SizedBox(height: 14),
          Text(AppLocalizations.of(context)?.sWhen ?? 'When', style: t.textTheme.labelLarge),
          const SizedBox(height: 6),
          Wrap(spacing: 8, runSpacing: 4, children: [
            for (var d = 0; d < 7; d++)
              ChoiceChip(
                key: Key('day_$d'),
                label: Text(d == 0 ? 'Today' : (d == 1 ? 'Tomorrow' : DateFormat('EEE d').format(DateTime.now().add(Duration(days: d))))),
                selected: _day == d,
                onSelected: (_) => setState(() => _day = d),
              ),
          ]),
          const SizedBox(height: 8),
          Wrap(spacing: 8, runSpacing: 4, children: [
            for (final h in const [12, 14, 17, 19, 20, 21])
              ChoiceChip(key: Key('hour_$h'), label: Text('$h:00'), selected: _hour == h, onSelected: (_) => setState(() => _hour = h)),
          ]),
          const SizedBox(height: 14),
          Row(children: [
            Text('People', style: t.textTheme.labelLarge),
            const Spacer(),
            IconButton(key: const Key('party_minus'), onPressed: _party > 1 ? () => setState(() => _party--) : null, icon: const Icon(Icons.remove_circle_outline)),
            Text('$_party', key: const Key('party_value'), style: t.textTheme.titleMedium),
            IconButton(key: const Key('party_plus'), onPressed: _party < 12 ? () => setState(() => _party++) : null, icon: const Icon(Icons.add_circle_outline)),
          ]),
          TextField(key: const Key('booking_note'), controller: _note, maxLength: 200, decoration: const InputDecoration(labelText: 'Anything they should know? (optional)')),
          const SizedBox(height: 8),
          FilledButton(
            key: const Key('booking_send'),
            onPressed: _busy ? null : _send,
            child: _busy ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2)) : Text(AppLocalizations.of(context)?.sSendRequest ?? 'Send request'),
          ),
        ]),
      ),
    );
  }
}

class _Bookings extends ConsumerWidget {
  const _Bookings();

  static const _labels = {
    'requested': 'Waiting for confirmation',
    'confirmed': 'Confirmed',
    'declined': 'Declined',
    'cancelled': 'Cancelled',
    'completed': 'Completed',
  };

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return AsyncListView<Booking>(
      value: ref.watch(myBookingsProvider),
      onRefresh: () async => ref.refresh(myBookingsProvider.future),
      emptyTitle: 'No bookings yet',
      emptyMessage: 'Pick a place and send a booking request.',
      emptyIcon: Icons.event_available_outlined,
      itemBuilder: (c, b) => Card(
        child: ListTile(
          key: Key('booking_${b.id}'),
          title: Text(b.partner),
          subtitle: Text(
            '${_when(b.requestedFor)} · ${b.partySize} ${b.partySize == 1 ? 'person' : 'people'}\n'
            '${_labels[b.status] ?? b.status}${b.price.isNotEmpty ? ' · ${b.price}' : ''}'
            '${b.paymentStatus == 'paid' ? ' · Paid' : (b.paymentStatus == 'refunded' ? ' · Refunded' : '')}',
          ),
          isThreeLine: true,
          trailing: b.payable
              ? FilledButton(
                  key: Key('pay_${b.id}'),
                  onPressed: () async {
                    String? url;
                    final ok = await guarded(c, () async => url = await ref.read(partnersRepositoryProvider).payBooking(b.id));
                    if (ok && url != null) {
                      await ref.read(urlOpenerProvider).open(url!);
                      if (c.mounted) showMessage(c, 'Pull down to refresh once you have paid.');
                    }
                  },
                  child: Text('Pay ${b.price}'),
                )
              : b.cancellable
              ? TextButton(
                  key: Key('cancel_${b.id}'),
                  onPressed: () async {
                    if (await guarded(c, () => ref.read(partnersRepositoryProvider).cancelBooking(b.id))) ref.invalidate(myBookingsProvider);
                  },
                  child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel'),
                )
              : null,
        ),
      ),
    );
  }
}
