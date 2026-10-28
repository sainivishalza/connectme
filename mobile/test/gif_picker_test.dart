import 'dart:convert';
import 'dart:typed_data';

import 'package:connectme/core/providers.dart';
import 'package:connectme/features/chat/widgets/gif_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'helpers.dart';

// The smallest valid GIF (1x1).
final Uint8List tinyGif = base64Decode('R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7');

void main() {
  testWidgets('search, pick a GIF, and the bytes come back to be sent as an encrypted attachment', (t) async {
    final queries = <String?>[];
    final client = fakeClient((o) async {
      if (o.path == '/gifs/search') {
        queries.add(o.queryParameters['q'] as String?);
        final q = (o.queryParameters['q'] as String?) ?? '';
        return Reply(200, {
          'items': q == 'zzz'
              ? <Map<String, String>>[]
              : [
                  {'id': 'heart', 'title': 'Pulsing heart'},
                  {'id': 'star', 'title': 'Spinning star'},
                ],
          'provider': 'builtin',
        });
      }
      return Reply(404);
    });
    // Binary GIF bytes are served by overriding the bytes provider.
    Uint8List? picked;
    await t.pumpWidget(ProviderScope(
      overrides: [
        apiClientProvider.overrideWithValue(client),
        gifBytesProvider.overrideWith((ref, id) async => tinyGif),
      ],
      child: MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (c) => TextButton(onPressed: () async => picked = await showGifPicker(c), child: const Text('open')),
          ),
        ),
      ),
    ));
    await t.tap(find.text('open'));
    await t.pumpAndSettle();
    expect(queries, [null], reason: 'opening shows popular GIFs without sending any search words');
    expect(find.byKey(const Key('gif_heart')), findsOneWidget);
    expect(find.byKey(const Key('gif_star')), findsOneWidget);

    await t.enterText(find.byKey(const Key('gif_search')), 'zzz');
    await t.pump(const Duration(milliseconds: 500));
    await t.pumpAndSettle();
    expect(queries.last, 'zzz');
    expect(find.byKey(const Key('no_gifs')), findsOneWidget);

    await t.enterText(find.byKey(const Key('gif_search')), 'love');
    await t.pump(const Duration(milliseconds: 500));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('gif_heart')));
    await t.pumpAndSettle();
    expect(picked, tinyGif);
  });

  testWidgets('when GIFs are unavailable the sheet says so instead of spinning', (t) async {
    final client = fakeClient((o) async => Reply(502, {'error': 'GIF search is unavailable right now'}));
    await t.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client)],
      child: MaterialApp(home: Scaffold(body: Builder(builder: (c) => TextButton(onPressed: () => showGifPicker(c), child: const Text('open'))))),
    ));
    await t.tap(find.text('open'));
    await t.pumpAndSettle();
    expect(find.text('GIF search is unavailable right now'), findsOneWidget);
  });
}
