import 'package:flutter/material.dart';

/// Shows short "you sent a gift" toasts floating over the video. Push a label
/// with [GiftOverlayController.show]; each one fades out on its own.
class GiftOverlayController extends ChangeNotifier {
  final List<({int id, String text})> _items = [];
  final List<({int id, IconData icon})> _bursts = [];
  int _next = 0;
  bool _disposed = false;

  List<({int id, String text})> get items => List.unmodifiable(_items);
  List<({int id, IconData icon})> get bursts => List.unmodifiable(_bursts);

  /// The picture that flies up the screen for a gift, chosen from its name.
  static IconData iconFor(String text) {
    final t = text.toLowerCase();
    if (t.contains('rose') || t.contains('flower')) return Icons.local_florist;
    if (t.contains('champagne') || t.contains('wine') || t.contains('drink')) {
      return Icons.wine_bar;
    }
    if (t.contains('heart')) return Icons.favorite;
    if (t.contains('star')) return Icons.star;
    if (t.contains('crown')) return Icons.workspace_premium;
    return Icons.card_giftcard;
  }

  void show(String text) {
    if (_disposed) return;
    final id = _next++;
    _items.add((id: id, text: text));
    _bursts.add((id: id, icon: iconFor(text)));
    notifyListeners();
    Future<void>.delayed(const Duration(milliseconds: 2200), () {
      if (_disposed) return;
      _bursts.removeWhere((e) => e.id == id);
      notifyListeners();
    });
    Future<void>.delayed(const Duration(seconds: 3), () {
      if (_disposed) return;
      _items.removeWhere((e) => e.id == id);
      notifyListeners();
    });
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}

class GiftOverlay extends StatelessWidget {
  const GiftOverlay({super.key, required this.controller});
  final GiftOverlayController controller;

  @override
  Widget build(BuildContext context) {
    return IgnorePointer(
      child: ListenableBuilder(
        listenable: controller,
        builder: (context, _) => Stack(
          clipBehavior: Clip.none,
          children: [
            if (!MediaQuery.disableAnimationsOf(context))
              for (final b in controller.bursts)
                _Burst(
                  key: ValueKey('gift_burst_${b.id}'),
                  icon: b.icon,
                  seed: b.id,
                ),
            Column(
              mainAxisAlignment: MainAxisAlignment.end,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                for (final g in controller.items)
                  Container(
                    key: ValueKey('gift_toast_${g.id}'),
                    margin: const EdgeInsets.only(bottom: 6),
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 6,
                    ),
                    decoration: BoxDecoration(
                      color: Colors.black54,
                      borderRadius: BorderRadius.circular(16),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Icon(
                          Icons.card_giftcard,
                          color: Colors.pinkAccent,
                          size: 18,
                        ),
                        const SizedBox(width: 6),
                        Text(
                          g.text,
                          style: const TextStyle(color: Colors.white),
                        ),
                      ],
                    ),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

/// One gift flying up from the bottom, growing a little and fading out.
class _Burst extends StatelessWidget {
  const _Burst({super.key, required this.icon, required this.seed});
  final IconData icon;
  final int seed;

  @override
  Widget build(BuildContext context) {
    final drift = ((seed % 5) - 2) * 18.0;
    return Positioned.fill(
      child: TweenAnimationBuilder<double>(
        tween: Tween(begin: 0, end: 1),
        duration: const Duration(milliseconds: 2000),
        curve: Curves.easeOut,
        builder: (context, v, _) => Align(
          alignment: Alignment(0.0 + drift / 200, 1.0 - 2.4 * v),
          child: Opacity(
            opacity: (1 - v).clamp(0.0, 1.0),
            child: Transform.scale(
              scale: 0.8 + v * 0.8,
              child: Icon(icon, size: 64, color: Colors.pinkAccent),
            ),
          ),
        ),
      ),
    );
  }
}
