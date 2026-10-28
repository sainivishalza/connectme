import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'core/router/app_router.dart';
import 'l10n/app_localizations.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'core/settings/display_settings.dart';
import 'core/theme/app_theme.dart';
import 'shared/widgets/ambient_frame.dart';

class ConnectMeApp extends ConsumerWidget {
  const ConnectMeApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final prefs = ref.watch(displayPrefsProvider);
    return MaterialApp.router(
      title: 'ConnectMe',
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      themeMode: prefs.theme,
      locale: prefs.language == null ? null : Locale(prefs.language!),
      supportedLocales: AppLocalizations.supportedLocales,
      localizationsDelegates: const [
        AppLocalizations.delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
      routerConfig: ref.watch(routerProvider),
      builder: (context, child) {
        // The member's text size multiplies the phone's own setting; the result
        // stays within a range where screens still lay out.
        final mq = MediaQuery.of(context);
        final system = mq.textScaler.scale(100) / 100;
        return MediaQuery(
          data: mq.copyWith(
            textScaler: TextScaler.linear((system * prefs.textScale).clamp(0.8, 2.4)),
            disableAnimations: mq.disableAnimations || prefs.reduceMotion,
          ),
          child: AmbientFrame(child: child ?? const SizedBox.shrink()),
        );
      },
      debugShowCheckedModeBanner: false,
    );
  }
}
