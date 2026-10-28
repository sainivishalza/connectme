import 'package:connectme/core/providers.dart';
import 'package:connectme/features/group_dates/screens/group_dates_screen.dart';
import 'package:connectme/features/payments/payments_repository.dart' show UrlOpener, urlOpenerProvider;
import 'package:connectme/features/social/screens/feed_screen.dart';
import 'package:connectme/features/social/social_repository.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';
import 'social_test.dart' show postJson;

class _Opener implements UrlOpener {
  _Opener(this.log);
  final List<String> log;
  @override
  Future<bool> open(String url) async {
    log.add(url);
    return true;
  }
}

Future<void> pump(WidgetTester t, Handler h, Widget home, {List<Override> extra = const []}) async {
  await t.pumpWidget(ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(fakeClient(h)), ...extra],
    child: MaterialApp(home: home),
  ));
  await t.pumpAndSettle();
}

void main() {
  testWidgets('group date: pay for the plan once, then read it; broke shows the error', (t) async {
    var paid = false;
    var broke = false;
    var charges = 0;
    await pump(t, (o) async {
      if (o.path == '/groups/matches/m1/dates') {
        return Reply(200, {
          'dates': [
            {'id': 'd1', 'venue': 'Taco Alley', 'address': '', 'starts_at': DateTime.now().add(const Duration(days: 2)).toUtc().toIso8601String(), 'status': 'confirmed', 'i_confirmed': true}
          ]
        });
      }
      if (o.path == '/groups/dates/d1/logistics' && o.method == 'GET') {
        return Reply(200, {'price_coins': 100, 'paid': paid, if (paid) 'plan': _plan});
      }
      if (o.path == '/groups/dates/d1/logistics' && o.method == 'POST') {
        if (broke) return Reply(402, {'error': 'not enough coins'});
        paid = true;
        charges++;
        return Reply(200, {'price_coins': 100, 'paid': true, 'plan': _plan});
      }
      return Reply(404);
    }, const GroupDatesScreen(groupId: 'g1', matchId: 'm1'));

    broke = true;
    await t.tap(find.text('Taco Alley'));
    await t.pumpAndSettle();
    expect(find.textContaining('100 coins'), findsWidgets);
    await t.tap(find.byKey(const Key('pay_logistics')));
    await t.pumpAndSettle();
    expect(find.textContaining('not enough coins'), findsOneWidget);
    expect(charges, 0);

    broke = false;
    await t.tap(find.text('Taco Alley'));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('pay_logistics')));
    await t.pumpAndSettle();
    expect(find.text('Meet at 7 and share starters'), findsOneWidget);
    expect(find.text('Ask about the best meal abroad'), findsOneWidget);
    expect(charges, 1);

    // Already paid: opens the plan directly, no second prompt.
    await t.tapAt(const Offset(10, 10));
    await t.pumpAndSettle();
    await t.tap(find.text('Taco Alley'));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('pay_logistics')), findsNothing);
    expect(find.text('Meet at 7 and share starters'), findsOneWidget);
  });

  testWidgets('feed: a sponsored card is labelled and opens through the click endpoint; none means no card', (t) async {
    final opened = <String>[];
    var clicks = 0;
    var ad = true;
    Future<Reply> h(o) async {
      if (o.path == '/sponsors/feed') {
        return Reply(200, {
          'placement': ad ? {'id': 's1', 'advertiser': 'Casa Verde', 'headline': 'Two for one dinner', 'body': 'Tonight only', 'link_url': 'https://casaverde.example.com/o'} : null
        });
      }
      if (o.path == '/sponsors/s1/click') {
        clicks++;
        return Reply(200, {'link_url': 'https://casaverde.example.com/o'});
      }
      return Reply(200, {'posts': [postJson('p1', body: 'Hello world')]});
    }

    final extra = [urlOpenerProvider.overrideWithValue(_Opener(opened))];
    await pump(t, h, const FeedScreen(kind: PostKind.moment), extra: extra);
    expect(find.text('Sponsored · Casa Verde'), findsOneWidget);
    expect(find.text('Hello world'), findsOneWidget);
    await t.tap(find.byKey(const Key('sponsored_card')));
    await t.pumpAndSettle();
    expect(clicks, 1);
    expect(opened, ['https://casaverde.example.com/o']);

    ad = false;
    await pump(t, h, FeedScreen(key: UniqueKey(), kind: PostKind.universe), extra: extra);
    expect(find.byKey(const Key('sponsored_card')), findsNothing);
    expect(find.text('Hello world'), findsOneWidget);

    // Missed connections never carry ads, so the endpoint is not even asked.
    ad = true;
    await pump(t, h, FeedScreen(key: UniqueKey(), kind: PostKind.missed), extra: extra);
    expect(find.byKey(const Key('sponsored_card')), findsNothing);
  });
}

const _plan = {
  'venues': ['Rooftop bar'],
  'icebreakers': ['Ask about the best meal abroad'],
  'plan': ['Meet at 7 and share starters'],
  'source': 'template'
};
