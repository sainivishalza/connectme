import 'package:connectme/features/live/widgets/gift_overlay.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('the gift picture follows the gift name', () {
    expect(GiftOverlayController.iconFor('Ana sent a Rose'), Icons.local_florist);
    expect(GiftOverlayController.iconFor('Ben sent Champagne'), Icons.wine_bar);
    expect(GiftOverlayController.iconFor('Cy sent a Heart'), Icons.favorite);
    expect(GiftOverlayController.iconFor('Di sent something'), Icons.card_giftcard);
  });

  testWidgets('a gift flies up as a burst and goes away by itself', (t) async {
    final c = GiftOverlayController();
    addTearDown(c.dispose);
    await t.pumpWidget(MaterialApp(home: Scaffold(body: SizedBox(height: 600, child: GiftOverlay(controller: c)))));
    c.show('Ana sent a Rose');
    await t.pump();
    expect(find.byKey(const ValueKey('gift_burst_0')), findsOneWidget);
    expect(find.byKey(const ValueKey('gift_toast_0')), findsOneWidget);
    await t.pump(const Duration(milliseconds: 2300));
    expect(find.byKey(const ValueKey('gift_burst_0')), findsNothing);
    await t.pump(const Duration(seconds: 1));
    expect(find.byKey(const ValueKey('gift_toast_0')), findsNothing);
  });
}
