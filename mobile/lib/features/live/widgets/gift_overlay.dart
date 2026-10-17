import 'package:flutter/material.dart';

/// Shows short "you sent a gift" toasts floating over the video. Push a label
/// with [GiftOverlayController.show]; each one fades out on its own.
class GiftOverlayController extends ChangeNotifier {
  final List<({int id, String text})> _items = [];
  int _next = 0;
  bool _disposed = false;

  List<({int id, String text})> get items => List.unmodifiable(_items);

  void show(String text) {
    if (_disposed) return;
    final id = _next++;
    _items.add((id: id, text: text));
    notifyListeners();
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
        builder: (context, _) => Column(
          mainAxisAlignment: MainAxisAlignment.end,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            for (final g in controller.items)
              Container(
                key: ValueKey('gift_toast_${g.id}'),
                margin: const EdgeInsets.only(bottom: 6),
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                decoration: BoxDecoration(color: Colors.black54, borderRadius: BorderRadius.circular(16)),
                child: Row(mainAxisSize: MainAxisSize.min, children: [
                  const Icon(Icons.card_giftcard, color: Colors.pinkAccent, size: 18),
                  const SizedBox(width: 6),
                  Text(g.text, style: const TextStyle(color: Colors.white)),
                ]),
              ),
          ],
        ),
      ),
    );
  }
}
