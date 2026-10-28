import 'package:flutter/material.dart';

import 'tokens.dart';

/// ConnectMe theme: quiet porcelain/ink surfaces, a serif for titles, and
/// colour reserved for the visibility modes (see tokens.dart).
class AppTheme {
  AppTheme._();

  static ThemeData light() => _build(Brightness.light);
  static ThemeData dark() => _build(Brightness.dark);

  static ColorScheme _scheme(Brightness b) {
    final light = b == Brightness.light;
    return ColorScheme(
      brightness: b,
      primary: light ? Palette.midnight : Palette.nightText,
      onPrimary: light ? Palette.paper : Palette.night,
      primaryContainer: light ? Palette.mist : Palette.nightLine,
      onPrimaryContainer: light ? Palette.midnight : Palette.nightText,
      secondary: ModeLight.daylight.solid,
      onSecondary: Palette.paper,
      secondaryContainer: light ? const Color(0xFFFFE3EA) : const Color(0xFF4A1B30),
      onSecondaryContainer: light ? const Color(0xFF5A1230) : const Color(0xFFFFD9E3),
      tertiary: light ? const Color(0xFF9A6F12) : const Color(0xFFE6C16A),
      onTertiary: Palette.paper,
      tertiaryContainer: light ? const Color(0xFFF6E7BF) : const Color(0xFF4A3A0E),
      onTertiaryContainer: light ? const Color(0xFF3F2E00) : const Color(0xFFF6E7BF),
      error: light ? Palette.danger : Palette.dangerOnDark,
      onError: light ? Palette.paper : Palette.night,
      surface: light ? Palette.porcelain : Palette.night,
      onSurface: light ? Palette.midnight : Palette.nightText,
      onSurfaceVariant: light ? Palette.slate : Palette.nightMuted,
      surfaceContainerLowest: light ? Palette.paper : Palette.night,
      surfaceContainerLow: light ? Palette.paper : Palette.nightRaised,
      surfaceContainer: light ? Palette.paper : Palette.nightRaised,
      surfaceContainerHigh: light ? Palette.mist : Palette.nightLine,
      surfaceContainerHighest: light ? Palette.mist : Palette.nightLine,
      outline: light ? const Color(0xFF666880) : Palette.nightMuted,
      outlineVariant: light ? Palette.mist : Palette.nightLine,
      shadow: Colors.black,
      inverseSurface: light ? Palette.midnight : Palette.nightText,
      onInverseSurface: light ? Palette.paper : Palette.night,
    );
  }

  static TextTheme _text(ColorScheme s) {
    TextStyle ui(double size, FontWeight w, {double h = 1.45, double ls = 0}) =>
        TextStyle(fontFamily: Fonts.ui, fontSize: size, fontWeight: w, height: h, letterSpacing: ls, color: s.onSurface);
    TextStyle display(double size, {double h = 1.12}) =>
        TextStyle(fontFamily: Fonts.display, fontSize: size, height: h, letterSpacing: -0.2, color: s.onSurface);
    return TextTheme(
      displayLarge: display(52, h: 1.05),
      displayMedium: display(44, h: 1.08),
      displaySmall: display(36),
      headlineLarge: display(32),
      headlineMedium: display(28),
      headlineSmall: display(24, h: 1.18),
      titleLarge: ui(21, FontWeight.w700, h: 1.3),
      titleMedium: ui(17, FontWeight.w600, h: 1.35),
      titleSmall: ui(15, FontWeight.w600, h: 1.35),
      bodyLarge: ui(16.5, FontWeight.w400, h: 1.5),
      bodyMedium: ui(15, FontWeight.w400, h: 1.5),
      bodySmall: ui(13, FontWeight.w400, h: 1.45).copyWith(color: s.onSurfaceVariant),
      labelLarge: ui(15.5, FontWeight.w600, h: 1.2),
      labelMedium: ui(13.5, FontWeight.w600, h: 1.2),
      labelSmall: ui(12, FontWeight.w500, h: 1.2),
    );
  }

  static ThemeData _build(Brightness b) {
    final s = _scheme(b);
    final t = _text(s);
    final light = b == Brightness.light;
    final hairline = BorderSide(color: s.outlineVariant);
    const stadium = StadiumBorder();

    return ThemeData(
      useMaterial3: true,
      brightness: b,
      colorScheme: s,
      fontFamily: Fonts.ui,
      textTheme: t,
      scaffoldBackgroundColor: s.surface,
      splashFactory: InkRipple.splashFactory,
      visualDensity: VisualDensity.standard,
      appBarTheme: AppBarTheme(
        backgroundColor: s.surface,
        surfaceTintColor: Colors.transparent,
        foregroundColor: s.onSurface,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        toolbarHeight: 64,
        titleSpacing: 20,
        titleTextStyle: t.headlineMedium,
        iconTheme: IconThemeData(color: s.onSurface),
        actionsIconTheme: IconThemeData(color: s.onSurface),
      ),
      cardTheme: CardThemeData(
        elevation: 0,
        margin: EdgeInsets.zero,
        color: s.surfaceContainerLow,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Radii.card), side: hairline),
      ),
      dividerTheme: DividerThemeData(color: s.outlineVariant, thickness: 1, space: 1),
      listTileTheme: ListTileThemeData(
        contentPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 2),
        iconColor: s.onSurface,
        titleTextStyle: t.titleSmall,
        subtitleTextStyle: t.bodySmall,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Radii.control)),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: s.surfaceContainerLow,
        contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 18),
        labelStyle: t.bodyMedium?.copyWith(color: s.onSurfaceVariant),
        floatingLabelStyle: t.labelMedium?.copyWith(color: s.onSurface),
        border: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.control), borderSide: hairline),
        enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.control), borderSide: hairline),
        focusedBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(Radii.control), borderSide: BorderSide(color: s.onSurface, width: 1.6)),
        errorBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.control), borderSide: BorderSide(color: s.error)),
        focusedErrorBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(Radii.control), borderSide: BorderSide(color: s.error, width: 1.6)),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: s.primary,
          foregroundColor: s.onPrimary,
          minimumSize: const Size.fromHeight(54),
          shape: stadium,
          textStyle: t.labelLarge,
          padding: const EdgeInsets.symmetric(horizontal: 28),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: s.onSurface,
          minimumSize: const Size(0, 48),
          shape: stadium,
          side: BorderSide(color: light ? const Color(0xFFC9CCD8) : Palette.nightLine, width: 1.2),
          textStyle: t.labelLarge,
          padding: const EdgeInsets.symmetric(horizontal: 22),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: s.onSurface,
          shape: stadium,
          textStyle: t.labelLarge,
          minimumSize: const Size(0, 44),
        ),
      ),
      iconButtonTheme: IconButtonThemeData(
        style: IconButton.styleFrom(foregroundColor: s.onSurface),
      ),
      floatingActionButtonTheme: FloatingActionButtonThemeData(
        backgroundColor: s.primary,
        foregroundColor: s.onPrimary,
        elevation: 3,
        highlightElevation: 4,
        extendedTextStyle: t.labelLarge,
        shape: stadium,
      ),
      chipTheme: ChipThemeData(
        shape: stadium,
        side: BorderSide.none,
        backgroundColor: s.surfaceContainerHigh,
        labelStyle: t.labelMedium,
        padding: const EdgeInsets.symmetric(horizontal: 6),
        iconTheme: IconThemeData(color: s.onSurfaceVariant, size: 16),
      ),
      segmentedButtonTheme: SegmentedButtonThemeData(
        style: ButtonStyle(
          shape: WidgetStatePropertyAll(stadium),
          side: WidgetStatePropertyAll(BorderSide.none),
          textStyle: WidgetStatePropertyAll(t.labelLarge),
          minimumSize: const WidgetStatePropertyAll(Size(0, 46)),
          backgroundColor: WidgetStateProperty.resolveWith(
              (st) => st.contains(WidgetState.selected) ? s.primary : Colors.transparent),
          foregroundColor: WidgetStateProperty.resolveWith(
              (st) => st.contains(WidgetState.selected) ? s.onPrimary : s.onSurface),
        ),
      ),
      tabBarTheme: TabBarThemeData(
        labelStyle: t.labelLarge,
        unselectedLabelStyle: t.labelLarge,
        labelColor: s.onSurface,
        unselectedLabelColor: s.onSurfaceVariant,
        indicatorSize: TabBarIndicatorSize.label,
        indicatorColor: s.secondary,
        dividerColor: Colors.transparent,
        overlayColor: const WidgetStatePropertyAll(Colors.transparent),
      ),
      navigationBarTheme: NavigationBarThemeData(
        height: 68,
        backgroundColor: s.surface,
        indicatorColor: s.secondaryContainer,
        surfaceTintColor: Colors.transparent,
        labelTextStyle: WidgetStatePropertyAll(t.labelSmall),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: s.surfaceContainerLow,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Radii.hero)),
        titleTextStyle: t.headlineSmall,
        contentTextStyle: t.bodyMedium,
      ),
      bottomSheetTheme: BottomSheetThemeData(
        backgroundColor: s.surfaceContainerLow,
        surfaceTintColor: Colors.transparent,
        showDragHandle: true,
        dragHandleColor: s.outlineVariant,
        shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(Radii.hero))),
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        backgroundColor: s.inverseSurface,
        contentTextStyle: t.bodyMedium?.copyWith(color: s.onInverseSurface),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Radii.control)),
      ),
      switchTheme: SwitchThemeData(
        thumbColor: WidgetStateProperty.resolveWith((st) => st.contains(WidgetState.selected) ? Palette.paper : s.outline),
        trackColor: WidgetStateProperty.resolveWith((st) => st.contains(WidgetState.selected) ? s.secondary : s.surfaceContainerHigh),
        trackOutlineColor: const WidgetStatePropertyAll(Colors.transparent),
      ),
      progressIndicatorTheme: ProgressIndicatorThemeData(color: s.secondary, strokeCap: StrokeCap.round),
    );
  }
}
