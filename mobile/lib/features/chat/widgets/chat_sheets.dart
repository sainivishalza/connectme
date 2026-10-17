import 'package:flutter/material.dart';

/// Disappearing-message choices, in seconds (must match the server's allowed set).
const burnOptions = {
  0: 'Off',
  5: '5 seconds',
  30: '30 seconds',
  300: '5 minutes',
  3600: '1 hour',
  86400: '1 day',
};

Future<int?> pickBurnSeconds(BuildContext context) => showModalBottomSheet<int>(
  context: context,
  builder: (c) => ListView(
    shrinkWrap: true,
    children: [
      const ListTile(title: Text('Disappear after reading')),
      for (final e in burnOptions.entries)
        ListTile(title: Text(e.value), onTap: () => Navigator.pop(c, e.key)),
    ],
  ),
);

/// Report reasons; keys match the server's accepted values.
const reportReasons = {
  'harassment': 'Harassment',
  'spam': 'Spam',
  'scam': 'Scam',
  'fake_profile': 'Fake profile',
  'inappropriate_content': 'Inappropriate content',
  'underage': 'Appears under 18',
  'other': 'Other',
};

Future<String?> pickReportReason(BuildContext context) =>
    showModalBottomSheet<String>(
      context: context,
      builder: (c) => ListView(
        shrinkWrap: true,
        children: [
          const ListTile(title: Text('Report this person')),
          for (final e in reportReasons.entries)
            ListTile(
              title: Text(e.value),
              onTap: () => Navigator.pop(c, e.key),
            ),
        ],
      ),
    );
