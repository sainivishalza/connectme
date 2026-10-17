import 'package:connectme/core/models.dart';
import 'package:connectme/core/providers.dart';
import 'package:connectme/core/router/app_router.dart';
import 'package:connectme/core/validators.dart';
import 'package:connectme/features/auth/auth_controller.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('Validators', () {
    test('email', () {
      expect(Validators.email('a@b.co'), isNull);
      expect(Validators.email(' '), isNotNull);
      expect(Validators.email('nope'), isNotNull);
      expect(Validators.email('a@b'), isNotNull);
    });

    test('password mirrors the server rules', () {
      expect(Validators.password('Correct-Horse-9'), isNull);
      for (final bad in ['short1A!', 'alllowercase-9-x', 'ALLUPPERCASE-9-X', 'NoDigitsHere-xx', 'NoSymbolsHere99']) {
        expect(Validators.password(bad), isNotNull, reason: bad);
      }
      expect(Validators.password('Aa1!${'x' * 80}'), isNotNull, reason: 'over 72 bytes');
    });

    test('18+ gate uses the conservative UTC-12 calendar', () {
      // 2026-10-16 02:00 at UTC+8 is still 2026-10-15 on the UTC-12 calendar.
      final now = DateTime.utc(2026, 10, 15, 18);
      expect(Validators.isAdult(DateTime(2008, 10, 15), now), isTrue);
      expect(Validators.isAdult(DateTime(2008, 10, 16), now), isFalse);
      expect(Validators.isAdult(DateTime(2010, 1, 1), now), isFalse);
      expect(Validators.ageOn(DateTime(2000, 2, 29), DateTime(2026, 2, 28)), 25);
    });

    test('iso date is zero padded', () => expect(Validators.isoDate(DateTime(2001, 3, 9)), '2001-03-09'));
  });

  group('route guard', () {
    test('unknown session shows the splash', () {
      expect(redirectFor(AuthStatus.unknown, '/discover'), '/');
      expect(redirectFor(AuthStatus.unknown, '/'), isNull);
    });
    test('signed out users can only see auth pages', () {
      expect(redirectFor(AuthStatus.signedOut, '/chats/abc'), '/login');
      expect(redirectFor(AuthStatus.signedOut, '/profile/premium'), '/login');
      for (final p in ['/login', '/register', '/verify', '/forgot', '/reset']) {
        expect(redirectFor(AuthStatus.signedOut, p), isNull);
      }
    });
    test('signed in users are moved off auth pages', () {
      expect(redirectFor(AuthStatus.signedIn, '/login'), '/discover');
      expect(redirectFor(AuthStatus.signedIn, '/'), '/discover');
      expect(redirectFor(AuthStatus.signedIn, '/chats'), isNull);
    });
  });

  test('jwtSubject reads the sub claim and tolerates garbage', () {
    // header.payload.signature with payload {"sub":"user-42"}
    const token = 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyLTQyIn0.sig';
    expect(jwtSubject(token), 'user-42');
    expect(jwtSubject('not-a-jwt'), isNull);
    expect(jwtSubject('a.b.c'), isNull);
  });

  group('models', () {
    test('anonymous profiles never produce a name or photo', () {
      final p = PublicProfile.fromJson({
        'ref': 'r1',
        'anonymous': true,
        'mode': 'incognito',
        'tags': ['hiking'],
        'intro': 'hello',
        'distance': 'within 5 km',
      });
      expect(p.title, 'Anonymous');
      expect(p.photos, isEmpty);
      expect(p.age, isNull);
      expect(p.tags, ['hiking']);
    });

    test('full profiles parse and tolerate missing fields', () {
      final p = PublicProfile.fromJson({'ref': 'r', 'anonymous': false, 'mode': 'open', 'display_name': 'Ana', 'age': 29, 'photos': ['/media/photos/1'], 'vip': true});
      expect(p.title, 'Ana');
      expect(p.age, 29);
      expect(p.vip, isTrue);
      expect(PublicProfile.fromJson({}).ref, '');
    });

    test('account and perks', () {
      final a = Account.fromJson({
        'coins': 120,
        'tier': 'gold',
        'perks': {'unlimited_likes': true, 'see_who_liked_you': true, 'super_likes_per_day': 5},
        'coin_packs': [
          {'id': 'starter', 'coins': 50, 'price_cents': 99}
        ],
        'likes_left_today': null,
      });
      expect(a.coins, 120);
      expect(a.perks.seeWhoLiked, isTrue);
      expect(a.packs.single.price, '\$0.99');
      expect(a.likesLeftToday, isNull);
    });

    test('group custom activity uses its label', () {
      final g = Group.fromJson({'id': 'g', 'name': 'x', 'type': 'group', 'activity': 'custom', 'activity_label': 'board games', 'target_size': 4, 'member_count': 4, 'ready': true});
      expect(g.activityName, 'board games');
      expect(Group.fromJson({'activity': 'bar_crawl'}).activityName, 'bar crawl');
    });
  });
}
