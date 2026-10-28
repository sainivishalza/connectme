import 'package:connectme/core/models/groups.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('suggestions carry an optional plan and its source', () {
    final s = Suggestions.fromJson({
      'venues': ['A rooftop'],
      'icebreakers': ['Two truths and a lie'],
      'plan': ['Meet in a square', 'Walk to dinner'],
      'source': 'llm',
    });
    expect(s.plan, ['Meet in a square', 'Walk to dinner']);
    expect(s.source, 'llm');
    final old = Suggestions.fromJson({'venues': [], 'icebreakers': ['x'], 'source': 'fallback'});
    expect(old.plan, isEmpty);
  });
}
