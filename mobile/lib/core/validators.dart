/// Client-side checks that mirror the server's rules for fast feedback. The
/// server is the authority; these only avoid pointless round trips.
class Validators {
  Validators._();

  static String? email(String? v) {
    final s = (v ?? '').trim();
    if (s.isEmpty) return 'Enter your email';
    if (!RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$').hasMatch(s)) return 'Enter a valid email';
    return null;
  }

  /// 12+ characters with upper, lower, digit and symbol; at most 72 bytes.
  static String? password(String? v) {
    final s = v ?? '';
    if (s.length < 12) return 'At least 12 characters';
    if (s.codeUnits.length > 72) return 'At most 72 characters';
    if (!RegExp(r'[A-Z]').hasMatch(s)) return 'Add an uppercase letter';
    if (!RegExp(r'[a-z]').hasMatch(s)) return 'Add a lowercase letter';
    if (!RegExp(r'[0-9]').hasMatch(s)) return 'Add a number';
    if (!RegExp(r'[^A-Za-z0-9\s]').hasMatch(s)) return 'Add a symbol';
    return null;
  }

  static String? required(String? v, [String what = 'This field']) =>
      (v ?? '').trim().isEmpty ? '$what is required' : null;

  /// Whole years between [dob] and [now].
  static int ageOn(DateTime dob, DateTime now) {
    var y = now.year - dob.year;
    if (now.month < dob.month || (now.month == dob.month && now.day < dob.day)) y--;
    return y;
  }

  /// Conservative 18+ check: uses the UTC-12 calendar like the server, so the
  /// app never offers a date the server would refuse.
  static bool isAdult(DateTime dob, [DateTime? now]) {
    final ref = (now ?? DateTime.now()).toUtc().subtract(const Duration(hours: 12));
    return ageOn(dob, ref) >= 18;
  }

  static String isoDate(DateTime d) =>
      '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';
}
