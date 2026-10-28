import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app.dart';
import 'core/network/api_client.dart';
import 'features/push/fcm_token_source.dart';
import 'features/push/push_source.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final push = await initPush();
  runApp(ProviderScope(retry: appRetry, overrides: [pushTokenSourceProvider.overrideWithValue(push)], child: const ConnectMeApp()));
}
