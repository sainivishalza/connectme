import 'package:connectme/core/settings/display_settings.dart';
import 'package:connectme/features/settings/appearance_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

class MemoryDisplayStore implements DisplayStore {
  MemoryDisplayStore([this.saved = const DisplayPrefs()]);
  DisplayPrefs saved;
  int writes = 0;
  @override
  Future<DisplayPrefs> read() async => saved;
  @override
  Future<void> write(DisplayPrefs p) async {
    saved = p;
    writes++;
  }
}

Future<ProviderContainer> pump(WidgetTester t, MemoryDisplayStore store) async {
  t.view.physicalSize = const Size(800, 1800);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.reset);
  final c = ProviderContainer(overrides: [displayStoreProvider.overrideWithValue(store)]);
  addTearDown(c.dispose);
  await t.pumpWidget(UncontrolledProviderScope(container: c, child: const MaterialApp(home: AppearanceScreen())));
  await t.pumpAndSettle();
  return c;
}

void main() {
  testWidgets('choices apply at once and are saved on the device', (t) async {
    final store = MemoryDisplayStore();
    final c = await pump(t, store);
    expect(c.read(displayPrefsProvider).theme, ThemeMode.system);

    await t.tap(find.text('Dark'));
    await t.pumpAndSettle();
    expect(c.read(displayPrefsProvider).theme, ThemeMode.dark);

    await t.tap(find.text('Larger'));
    await t.pumpAndSettle();
    expect(c.read(displayPrefsProvider).textScale, 1.3);

    await t.tap(find.byKey(const Key('reduce_motion')));
    await t.pumpAndSettle();
    expect(c.read(displayPrefsProvider).reduceMotion, isTrue);

    expect(store.saved.theme, ThemeMode.dark);
    expect(store.saved.textScale, 1.3);
    expect(store.saved.reduceMotion, isTrue);
    expect(store.writes, 3);
  });

  testWidgets('saved choices are restored when the app starts', (t) async {
    final c = await pump(t, MemoryDisplayStore(const DisplayPrefs(theme: ThemeMode.light, textScale: 1.5, reduceMotion: true)));
    await t.pumpAndSettle();
    final p = c.read(displayPrefsProvider);
    expect((p.theme, p.textScale, p.reduceMotion), (ThemeMode.light, 1.5, true));
    expect(t.widget<SwitchListTile>(find.byKey(const Key('reduce_motion'))).value, isTrue);
  });

  test('only known text sizes are accepted from storage', () {
    expect(DisplayPrefs.textScales, [1.0, 1.15, 1.3, 1.5]);
  });
}
