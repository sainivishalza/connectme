import 'package:connectme/core/providers.dart';
import 'package:connectme/features/chapters/widgets/chapters_section.dart';
import 'package:connectme/features/partners/screens/events_screen.dart';
import 'package:connectme/features/payments/payments_repository.dart' show UrlOpener, urlOpenerProvider;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

Map<String, dynamic> eventJson(String id, {String title = 'Rooftop mixer', int price = 0, int going = 3, int? capacity, bool joined = false, String kind = 'mixer', bool guests = true}) => {
      'id': id,
      'partner': 'Grand Hotel',
      'kind': kind,
      'title': title,
      'description': 'Drinks on the roof',
      'starts_at': DateTime.now().add(const Duration(days: 2)).toUtc().toIso8601String(),
      'ends_at': DateTime.now().add(const Duration(days: 2, hours: 3)).toUtc().toIso8601String(),
      'capacity': capacity,
      'going': going,
      'price_coins': price,
      'guests_only': guests,
      'joined': joined,
    };

Future<FakeAdapter> pump(WidgetTester t, Handler h, Widget home, {List<Override> extra = const []}) async {
  final client = fakeClient(h);
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client), ...extra],
    child: MaterialApp(home: home),
  ));
  await t.pumpAndSettle();
  return client.dio.httpClientAdapter as FakeAdapter;
}

void main() {
  testWidgets('events: join a paid event and leave with a refund note, full events cannot be joined', (t) async {
    var joined = false;
    var calls = <String>[];
    await pump(t, (o) async {
      calls.add('${o.method} ${o.path}');
      if (o.path == '/partners/events') {
        return Reply(200, {
          'events': [
            eventJson('e1', price: 30, capacity: 5, joined: joined),
            eventJson('e2', title: 'Sold out gig', kind: 'concert', going: 10, capacity: 10, guests: false),
          ]
        });
      }
      if (o.path == '/partners/events/e1/rsvp' && o.method == 'POST') {
        joined = true;
        return Reply(200, eventJson('e1', price: 30, joined: true));
      }
      if (o.path == '/partners/events/e1/rsvp' && o.method == 'DELETE') {
        joined = false;
        return Reply(204);
      }
      if (o.path == '/partners/nearby') return Reply(200, {'partners': []});
      if (o.path == '/partners/bookings/mine') return Reply(200, {'bookings': []});
      return Reply(404);
    }, const EventsScreen());
    expect(find.text('Rooftop mixer'), findsOneWidget);
    expect(find.textContaining('3 going of 5 · 30 coins · hotel guests'), findsOneWidget);
    expect(find.text('Full'), findsOneWidget);
    final full = t.widget<FilledButton>(find.ancestor(of: find.text('Full'), matching: find.byType(FilledButton)));
    expect(full.onPressed, isNull);

    await t.tap(find.byKey(const Key('join_e1')));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('leave_e1')), findsOneWidget);
    await t.tap(find.byKey(const Key('leave_e1')));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('join_e1')), findsOneWidget);
    expect(calls.where((c) => c.endsWith('/rsvp')).toList(), ['POST /partners/events/e1/rsvp', 'DELETE /partners/events/e1/rsvp']);
  });

  testWidgets('not enough coins is explained', (t) async {
    await pump(t, (o) async {
      if (o.path == '/partners/events') {
        return Reply(200, {
          'events': [eventJson('e1', price: 30)]
        });
      }
      if (o.path == '/partners/events/e1/rsvp') return Reply(402, {'error': 'insufficient coins'});
      return Reply(200, {'partners': [], 'bookings': []});
    }, const EventsScreen());
    await t.tap(find.byKey(const Key('join_e1')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 100));
    expect(find.text('insufficient coins'), findsOneWidget);
  });

  testWidgets('places: send a booking request, then see and cancel it', (t) async {
    Map? sent;
    var booked = false;
    await pump(t, (o) async {
      if (o.path == '/partners/events') return Reply(200, {'events': []});
      if (o.path == '/partners/nearby') {
        return Reply(200, {
          'partners': [
            {'id': 'p1', 'kind': 'restaurant', 'name': 'Casa Verde', 'description': '', 'address': '1 Plaza', 'distance': 'within 2 km'}
          ]
        });
      }
      if (o.path == '/partners/p1/bookings') {
        sent = o.data as Map;
        booked = true;
        return Reply(201, {'id': 'b1', 'partner': 'Casa Verde', 'party_size': 4, 'requested_for': sent!['requested_for'], 'status': 'requested', 'note': ''});
      }
      if (o.path == '/partners/bookings/mine') {
        return Reply(200, {
          'bookings': booked
              ? [
                  {'id': 'b1', 'partner': 'Casa Verde', 'party_size': 4, 'requested_for': sent!['requested_for'], 'status': 'requested', 'note': ''}
                ]
              : []
        });
      }
      if (o.path == '/partners/bookings/b1' && o.method == 'DELETE') {
        booked = false;
        return Reply(204);
      }
      return Reply(404);
    }, const EventsScreen());
    await t.tap(find.text('Places'));
    await t.pumpAndSettle();
    expect(find.text('Casa Verde'), findsOneWidget);
    await t.tap(find.byKey(const Key('book_p1')));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('party_plus')));
    await t.tap(find.byKey(const Key('party_plus')));
    await t.tap(find.byKey(const Key('hour_20')));
    await t.tap(find.byKey(const Key('day_2')));
    await t.enterText(find.byKey(const Key('booking_note')), 'Window seat');
    await t.pump();
    await t.tap(find.byKey(const Key('booking_send')));
    await t.pumpAndSettle();
    expect(sent?['party_size'], 4);
    expect(sent?['note'], 'Window seat');
    final at = DateTime.parse(sent!['requested_for'] as String).toLocal();
    expect(at.hour, 20);
    expect(at.isAfter(DateTime.now().add(const Duration(days: 1))), isTrue);

    await t.tap(find.text('My bookings'));
    await t.pumpAndSettle();
    expect(find.textContaining('Waiting for confirmation'), findsOneWidget);
    await t.tap(find.byKey(const Key('cancel_b1')));
    await t.pumpAndSettle();
    expect(find.text('No bookings yet'), findsOneWidget);
  });

  testWidgets('chapters: add, edit and save, then readers see them', (t) async {
    Map? saved;
    await pump(t, (o) async {
      if (o.path == '/chapters/me' && o.method == 'GET') return Reply(200, {'chapters': []});
      if (o.path == '/chapters/me' && o.method == 'PUT') {
        saved = o.data as Map;
        return Reply(200, {'chapters': saved!['chapters']});
      }
      return Reply(404);
    }, const Scaffold(body: SingleChildScrollView(child: ChaptersSection())));
    expect(find.byKey(const Key('chapter_0')), findsNothing);
    for (var i = 0; i < 6; i++) {
      if (find.byKey(const Key('add_chapter')).evaluate().isNotEmpty) {
        await t.ensureVisible(find.byKey(const Key('add_chapter')));
        await t.tap(find.byKey(const Key('add_chapter')));
        await t.pump();
      }
    }
    expect(find.byKey(const Key('chapter_4')), findsOneWidget);
    expect(find.byKey(const Key('add_chapter')), findsNothing, reason: 'five chapters at most');
    await t.enterText(find.byKey(const Key('chapter_title_0')), 'The year I moved abroad');
    await t.enterText(find.byKey(const Key('chapter_body_0')), 'Two bags and a plant.');
    // Remove the four empty chapters after the first one.
    for (var n = 0; n < 4; n++) {
      t.widget<IconButton>(find.byKey(const Key('chapter_remove_1'))).onPressed!();
      await t.pump();
    }
    await t.ensureVisible(find.byKey(const Key('save_chapters')));
    await t.tap(find.byKey(const Key('save_chapters')));
    await t.pumpAndSettle();
    expect(saved?['chapters'], [
      {'title': 'The year I moved abroad', 'body': 'Two bags and a plant.'}
    ]);
  });

  testWidgets('a confirmed booking shows its price and opens Checkout; a paid one shows Paid', (t) async {
    var paid = false;
    final opened = <String>[];
    Map<String, dynamic> booking() => {
          'id': 'b1',
          'partner': 'Casa Verde',
          'party_size': 2,
          'requested_for': DateTime.now().add(const Duration(days: 2)).toUtc().toIso8601String(),
          'status': 'confirmed',
          'amount_cents': 20000,
          'payment_status': paid ? 'paid' : 'unpaid',
        };
    await pump(
      t,
      (o) async {
        if (o.path == '/partners/events') return Reply(200, {'events': []});
        if (o.path == '/partners/nearby') return Reply(200, {'partners': []});
        if (o.path == '/partners/bookings/mine') return Reply(200, {'bookings': [booking()]});
        if (o.path == '/payments/bookings/b1/checkout') return Reply(201, {'session_id': 'cs_1', 'checkout_url': 'https://checkout.stripe.test/cs_1'});
        return Reply(404);
      },
      const EventsScreen(),
      extra: [urlOpenerProvider.overrideWithValue(_Opener(opened))],
    );
    await t.tap(find.text('My bookings'));
    await t.pumpAndSettle();
    expect(find.textContaining('Confirmed · \$200.00'), findsOneWidget);
    expect(find.text('Pay \$200.00'), findsOneWidget);
    await t.tap(find.byKey(const Key('pay_b1')));
    await t.pumpAndSettle();
    expect(opened, ['https://checkout.stripe.test/cs_1']);

    // After paying, a refresh shows it paid and no Pay button.
    paid = true;
    await t.drag(find.byType(ListView).last, const Offset(0, 400));
    await t.pumpAndSettle();
    expect(find.textContaining('Paid'), findsOneWidget);
    expect(find.byKey(const Key('pay_b1')), findsNothing);
  });
}

class _Opener implements UrlOpener {
  _Opener(this.log);
  final List<String> log;
  @override
  Future<bool> open(String url) async {
    log.add(url);
    return true;
  }
}
