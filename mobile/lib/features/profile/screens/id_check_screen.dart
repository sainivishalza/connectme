import 'dart:typed_data';

import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../id_check_repository.dart';
import '../profile_repository.dart';

/// The real camera picker. A selfie opens the front camera.
final photoPickerProvider = Provider<PhotoPicker>((ref) => ({required bool selfie}) async {
      final x = await ImagePicker().pickImage(
        source: ImageSource.camera,
        preferredCameraDevice: selfie ? CameraDevice.front : CameraDevice.rear,
        maxWidth: 1800,
        imageQuality: 85,
      );
      return x?.readAsBytes();
    });

/// Verify with a photo ID. A person on the ConnectMe team looks at the two
/// photos and decides; the photos are deleted as soon as they have.
class IdCheckScreen extends ConsumerStatefulWidget {
  const IdCheckScreen({super.key});

  @override
  ConsumerState<IdCheckScreen> createState() => _IdCheckScreenState();
}

class _IdCheckScreenState extends ConsumerState<IdCheckScreen> {
  Uint8List? _id;
  Uint8List? _selfie;
  bool _busy = false;

  Future<void> _start() async {
    setState(() => _busy = true);
    await guarded(context, () async {
      await ref.read(idCheckRepositoryProvider).start();
      ref.invalidate(idCheckStatusProvider);
    });
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _take({required bool selfie}) async {
    final bytes = await ref.read(photoPickerProvider)(selfie: selfie);
    if (bytes == null || !mounted) return;
    setState(() => selfie ? _selfie = bytes : _id = bytes);
  }

  Future<void> _send() async {
    final id = _id, me = _selfie;
    if (id == null || me == null) return;
    setState(() => _busy = true);
    final ok = await guarded(context, () async {
      await ref.read(idCheckRepositoryProvider).submit(idPhoto: id, selfie: me);
      ref.invalidate(idCheckStatusProvider);
      ref.invalidate(meProvider);
    });
    if (!mounted) return;
    setState(() {
      _busy = false;
      if (ok) {
        _id = null;
        _selfie = null;
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final status = ref.watch(idCheckStatusProvider);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.sVerifyWithId ?? 'Verify with ID')),
      body: status.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(idCheckStatusProvider)),
        data: (s) => ListView(padding: const EdgeInsets.all(16), children: _body(s)),
      ),
    );
  }

  List<Widget> _body(IdCheckStatus s) {
    final theme = Theme.of(context);
    switch (s.state) {
      case 'verified':
        return [
          const Icon(Icons.verified, size: 56),
          const SizedBox(height: 12),
          Text('You are ID verified', style: theme.textTheme.titleLarge),
          const SizedBox(height: 8),
          const Text('Thank you. The photos you sent have been deleted.'),
        ];
      case 'pending':
        return [
          const Icon(Icons.hourglass_top, size: 56),
          const SizedBox(height: 12),
          Text('We are checking your photos', style: theme.textTheme.titleLarge),
          const SizedBox(height: 8),
          const Text('A person on our team reviews each check. This can take up to a day. Your photos are deleted once they decide.'),
          const SizedBox(height: 16),
          OutlinedButton(onPressed: () => ref.invalidate(idCheckStatusProvider), child: Text(AppLocalizations.of(context)?.sRefresh ?? 'Refresh')),
        ];
      case 'awaiting':
        return [
          Text('Take two photos', style: theme.textTheme.titleLarge),
          const SizedBox(height: 12),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(children: [
                const Text('Write this code on a piece of paper:'),
                const SizedBox(height: 8),
                Text(s.code, key: const Key('id_code'), style: theme.textTheme.displaySmall?.copyWith(letterSpacing: 8)),
                if (s.expiresAt != null) Text('Valid until ${TimeOfDay.fromDateTime(s.expiresAt!).format(context)}', style: theme.textTheme.bodySmall),
              ]),
            ),
          ),
          const SizedBox(height: 16),
          _step(1, 'Your ID', 'A passport, driving licence or national ID card. All four corners and your photo and date of birth must be readable.', _id,
              Icons.badge_outlined, () => _take(selfie: false), const Key('take_id')),
          _step(2, 'Your selfie', 'Hold the paper with the code next to your face.', _selfie, Icons.face_outlined, () => _take(selfie: true), const Key('take_selfie')),
          const SizedBox(height: 8),
          Text('Only our review team can see these photos, and they are deleted as soon as the check is decided. They are never shown on your profile.',
              style: theme.textTheme.bodySmall),
          const SizedBox(height: 16),
          FilledButton(
            key: const Key('send_id'),
            onPressed: _busy || _id == null || _selfie == null ? null : _send,
            child: Text(_busy ? 'Sending…' : 'Send for review'),
          ),
        ];
      default:
        return [
          if (s.state == 'rejected') ...[
            Card(
              color: theme.colorScheme.errorContainer,
              child: Padding(padding: const EdgeInsets.all(16), child: Text('Your last check was not approved. ${s.rejectionText}')),
            ),
            const SizedBox(height: 16),
          ],
          Text('Show you are who you say you are', style: theme.textTheme.titleLarge),
          const SizedBox(height: 8),
          const Text('Send a photo of your ID and a selfie holding a short code. A person on our team checks them and then deletes them. Members with a verified ID are shown with a badge on their own profile.'),
          const SizedBox(height: 16),
          FilledButton(
            key: const Key('start_id'),
            onPressed: _busy ? null : _start,
            child: Text(s.state == 'rejected' ? 'Try again' : 'Get my code'),
          ),
        ];
    }
  }

  Widget _step(int n, String title, String hint, Uint8List? photo, IconData icon, VoidCallback onTap, Key key) {
    return Card(
      child: ListTile(
        key: key,
        leading: photo == null ? Icon(icon) : const Icon(Icons.check_circle, color: Colors.green),
        title: Text('$n. $title'),
        subtitle: Text(photo == null ? hint : 'Photo taken. Tap to retake.'),
        onTap: _busy ? null : onTap,
      ),
    );
  }
}
