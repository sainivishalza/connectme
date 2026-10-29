import 'package:flutter/material.dart';

import '../../core/network/api_client.dart';
import '../../l10n/app_localizations.dart';

void showMessage(BuildContext context, String message) {
  ScaffoldMessenger.of(context)
    ..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(content: Text(message)));
}

void showError(BuildContext context, Object error) {
  final msg = error is ApiException && error.isModerationUnavailable
      ? (AppLocalizations.of(context)?.sModerationUnavailable ?? moderationUnavailableMessage)
      : error is ApiException
          ? error.message
          : 'Something went wrong. Please try again.';
  showMessage(context, msg);
}

/// Runs an action, surfacing API errors as a snackbar. Returns true on success.
Future<bool> guarded(BuildContext context, Future<void> Function() action) async {
  try {
    await action();
    return true;
  } catch (e) {
    if (context.mounted) showError(context, e);
    return false;
  }
}
