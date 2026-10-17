// Defensive JSON helpers shared by the model files: a missing or mistyped
// field never crashes the UI.

String jsonString(dynamic v) => v is String ? v : '';
int jsonInt(dynamic v) => v is num ? v.toInt() : 0;
bool jsonBool(dynamic v) => v is bool && v;
List<String> jsonStrings(dynamic v) => v is List ? v.whereType<String>().toList() : <String>[];
List<Map<String, dynamic>> jsonMaps(dynamic v) =>
    v is List ? v.whereType<Map>().map((e) => Map<String, dynamic>.from(e)).toList() : [];

List<T> listOf<T>(dynamic v, T Function(Map<String, dynamic>) f) => jsonMaps(v).map(f).toList();
