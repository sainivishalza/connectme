import 'package:connectme/core/realtime/live_refresh.dart';
import 'package:flutter_test/flutter_test.dart';

import 'chat_rig.dart';

void main() {
  test('polls fast with no relay, then pushes and slows down once watching', () async {
    var ticks = 0;
    final rt = FakeRealtime();
    final live = LiveRefresh(
      client: rt,
      kind: 'game',
      id: 'g1',
      interval: const Duration(milliseconds: 40),
      onTick: () => ticks++,
    ).start();
    await Future<void>.delayed(const Duration(milliseconds: 20));
    expect(rt.watching, ['game:g1']);

    // Accepted: the fast poll is replaced by a slow safety net (10x here).
    final before = ticks;
    await Future<void>.delayed(const Duration(milliseconds: 200));
    expect(ticks - before, lessThanOrEqualTo(1));

    // A signal for this topic refetches; another room's does not.
    rt.sig.add('game:other');
    await Future<void>.delayed(const Duration(milliseconds: 200));
    final quiet = ticks;
    rt.sig.add('game:g1');
    rt.sig.add('game:g1'); // coalesced
    await Future<void>.delayed(const Duration(milliseconds: 250));
    expect(ticks - quiet, 1);

    live.dispose();
    expect(rt.watching, isEmpty);
  });

  test('keeps polling at the fast rate when the relay refuses the watch', () async {
    var ticks = 0;
    final rt = FakeRealtime()..watchOk = false;
    final live = LiveRefresh(
      client: rt,
      kind: 'live',
      id: 'r1',
      interval: const Duration(milliseconds: 30),
      onTick: () => ticks++,
    ).start();
    await Future<void>.delayed(const Duration(milliseconds: 250));
    expect(ticks, greaterThanOrEqualTo(5));
    live.dispose();
  });

  test('without any relay it is plain polling', () async {
    var ticks = 0;
    final live = LiveRefresh(client: null, kind: 'game', id: 'g', interval: const Duration(milliseconds: 30), onTick: () => ticks++).start();
    await Future<void>.delayed(const Duration(milliseconds: 150));
    expect(ticks, greaterThanOrEqualTo(3));
    live.dispose();
  });
}
