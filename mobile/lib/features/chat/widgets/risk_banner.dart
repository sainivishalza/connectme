import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';

import '../data/safety_scan.dart';

/// A gentle, dismissible warning above the conversation when the other person's
/// recent messages look like a scam or pressure tactic.
class RiskBanner extends StatelessWidget {
  const RiskBanner({super.key, required this.risk, required this.onDismiss, required this.onReport, required this.onBlock, this.onAiCheck});
  final Risk risk;
  final VoidCallback onDismiss;
  final VoidCallback onReport;
  final VoidCallback onBlock;

  /// Offered when set: asks the AI service to look at the last few messages,
  /// only after the member agrees to share them.
  final VoidCallback? onAiCheck;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Material(
      key: const Key('risk_banner'),
      color: t.colorScheme.tertiaryContainer,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 10, 8, 8),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            Icon(Icons.shield_outlined, color: t.colorScheme.onTertiaryContainer),
            const SizedBox(width: 10),
            Expanded(child: Text(risk.title, style: t.textTheme.titleSmall?.copyWith(color: t.colorScheme.onTertiaryContainer))),
            IconButton(key: const Key('risk_dismiss'), tooltip: 'Dismiss', icon: const Icon(Icons.close), onPressed: onDismiss),
          ]),
          Text(risk.advice, style: t.textTheme.bodySmall?.copyWith(color: t.colorScheme.onTertiaryContainer)),
          Wrap(spacing: 8, children: [
            TextButton(key: const Key('risk_report'), onPressed: onReport, child: Text(AppLocalizations.of(context)?.sReport ?? 'Report')),
            if (onAiCheck != null) TextButton(key: const Key('risk_ai'), onPressed: onAiCheck, child: const Text('Look closer')),
            TextButton(key: const Key('risk_block'), onPressed: onBlock, child: Text(AppLocalizations.of(context)?.sBlock ?? 'Block')),
          ]),
        ]),
      ),
    );
  }
}
