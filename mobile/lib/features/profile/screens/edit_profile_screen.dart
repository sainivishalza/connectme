import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:image_picker/image_picker.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_button.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/avatar.dart';
import '../../../shared/widgets/states.dart';
import '../profile_repository.dart';
import '../../chapters/widgets/chapters_section.dart';
import '../../videos/widgets/reel_section.dart';
import '../widgets/voice_intro_section.dart';

class EditProfileScreen extends ConsumerWidget {
  const EditProfileScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final me = ref.watch(meProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Edit profile')),
      body: me.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(meProvider)),
        data: (p) => EditProfileForm(profile: p),
      ),
    );
  }
}

class EditProfileForm extends ConsumerStatefulWidget {
  const EditProfileForm({super.key, required this.profile});
  final OwnProfile profile;
  @override
  ConsumerState<EditProfileForm> createState() => _EditProfileFormState();
}

class _EditProfileFormState extends ConsumerState<EditProfileForm> {
  late final _name = TextEditingController(text: widget.profile.displayName);
  late final _bio = TextEditingController(text: widget.profile.bio);
  late final _intro = TextEditingController(text: widget.profile.intro);
  late final _mood = TextEditingController(text: widget.profile.mood);
  late final _tagInput = TextEditingController();
  late List<String> _tags = [...widget.profile.tags];
  late String _gender = widget.profile.gender;
  late String _lookingFor = widget.profile.lookingFor;
  bool _busy = false;

  @override
  void dispose() {
    for (final c in [_name, _bio, _intro, _mood, _tagInput]) {
      c.dispose();
    }
    super.dispose();
  }

  void _addTag() {
    final t = _tagInput.text.trim().toLowerCase();
    if (t.isEmpty || _tags.contains(t) || _tags.length >= 10) return;
    setState(() {
      _tags = [..._tags, t];
      _tagInput.clear();
    });
  }

  Future<void> _save() async {
    setState(() => _busy = true);
    final ok = await guarded(context, () async {
      await ref.read(profileRepositoryProvider).update({
        'display_name': _name.text,
        'bio': _bio.text,
        'intro': _intro.text,
        'mood': _mood.text,
        'tags': _tags,
        if (_gender.isNotEmpty) 'gender': _gender,
        if (_lookingFor.isNotEmpty) 'looking_for': _lookingFor,
      });
      ref.invalidate(meProvider);
    });
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      showMessage(context, 'Profile saved');
      context.pop();
    }
  }

  Future<void> _addPhoto() async {
    final x = await ImagePicker().pickImage(source: ImageSource.gallery, maxWidth: 2048, imageQuality: 90);
    if (x == null || !mounted) return;
    final bytes = await x.readAsBytes();
    if (!mounted) return;
    await guarded(context, () async {
      await ref.read(profileRepositoryProvider).uploadPhoto(bytes, x.name);
      ref.invalidate(meProvider);
    });
  }

  Future<void> _share() async {
    final ok = await guarded(context, () async {
      await shareLocation(ref);
      ref.invalidate(meProvider);
    });
    if (ok && mounted) showMessage(context, 'Location updated');
  }

  @override
  Widget build(BuildContext context) {
    final p = widget.profile;
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Text('Photos', style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 8),
        SizedBox(
          height: 96,
          child: ListView(
            scrollDirection: Axis.horizontal,
            children: [
              for (final ph in p.photos)
                Padding(
                  padding: const EdgeInsets.only(right: 8),
                  child: Stack(
                    children: [
                      Avatar(photoPath: ph.path, radius: 44),
                      Positioned(
                        right: -6,
                        top: -6,
                        child: IconButton(
                          tooltip: 'Remove photo',
                          icon: const Icon(Icons.cancel),
                          onPressed: () => guarded(context, () async {
                            await ref.read(profileRepositoryProvider).deletePhoto(ph.id);
                            ref.invalidate(meProvider);
                          }),
                        ),
                      ),
                    ],
                  ),
                ),
              if (p.photos.length < 6)
                InkWell(
                  key: const Key('add_photo'),
                  onTap: _addPhoto,
                  child: const CircleAvatar(radius: 44, child: Icon(Icons.add_a_photo_outlined)),
                ),
            ],
          ),
        ),
        const SizedBox(height: 20),
        VoiceIntroSection(profile: p),
        const SizedBox(height: 16),
        ReelSection(myRef: p.ref),
        const SizedBox(height: 16),
        const ChaptersSection(),
        const SizedBox(height: 16),
        TextField(controller: _name, decoration: const InputDecoration(labelText: 'Name'), maxLength: 50),
        TextField(controller: _intro, decoration: const InputDecoration(labelText: 'One-line intro'), maxLength: 140),
        TextField(controller: _bio, decoration: const InputDecoration(labelText: 'Bio'), maxLines: 4, maxLength: 500),
        TextField(controller: _mood, decoration: const InputDecoration(labelText: 'Mood'), maxLength: 50),
        const SizedBox(height: 8),
        DropdownButtonFormField<String>(
          initialValue: _gender.isEmpty ? null : _gender,
          decoration: const InputDecoration(labelText: 'I am'),
          items: const [
            DropdownMenuItem(value: 'woman', child: Text('Woman')),
            DropdownMenuItem(value: 'man', child: Text('Man')),
            DropdownMenuItem(value: 'nonbinary', child: Text('Non-binary')),
            DropdownMenuItem(value: 'other', child: Text('Other')),
          ],
          onChanged: (v) => setState(() => _gender = v ?? ''),
        ),
        const SizedBox(height: 16),
        DropdownButtonFormField<String>(
          initialValue: _lookingFor.isEmpty ? null : _lookingFor,
          decoration: const InputDecoration(labelText: 'Looking for'),
          items: const [
            DropdownMenuItem(value: 'women', child: Text('Women')),
            DropdownMenuItem(value: 'men', child: Text('Men')),
            DropdownMenuItem(value: 'everyone', child: Text('Everyone')),
          ],
          onChanged: (v) => setState(() => _lookingFor = v ?? ''),
        ),
        const SizedBox(height: 16),
        Text('Interests (shown even when you are incognito)', style: Theme.of(context).textTheme.titleSmall),
        Wrap(
          spacing: 8,
          children: [
            for (final t in _tags)
              InputChip(label: Text(t), onDeleted: () => setState(() => _tags = _tags.where((x) => x != t).toList())),
          ],
        ),
        Row(
          children: [
            Expanded(
              child: TextField(
                key: const Key('tag_input'),
                controller: _tagInput,
                decoration: const InputDecoration(labelText: 'Add an interest'),
                onSubmitted: (_) => _addTag(),
              ),
            ),
            IconButton(onPressed: _addTag, icon: const Icon(Icons.add)),
          ],
        ),
        const SizedBox(height: 16),
        ListTile(
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.my_location),
          title: Text(p.hasLocation ? 'Update my location' : 'Share my location'),
          subtitle: const Text('Others only ever see a rough distance.'),
          onTap: _share,
        ),
        const SizedBox(height: 16),
        AppButton(key: const Key('save_profile'), label: 'Save', busy: _busy, onPressed: _save),
      ],
    );
  }
}
