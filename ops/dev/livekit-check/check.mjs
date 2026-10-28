// usage: node check.mjs <ws-url> <speaker-token> <listener-token>
// Exit 0 only if the listener received audio frames from the speaker AND the
// listener's own attempt to publish was refused.
import { Room, RoomEvent, LocalAudioTrack, AudioSource, AudioFrame, TrackSource, TrackPublishOptions, TrackKind } from '@livekit/rtc-node';

const [url, speakerTok, listenerTok] = process.argv.slice(2);
const fail = (m) => { console.error('FAIL: ' + m); process.exit(1); };
setTimeout(() => fail('timeout'), 40000).unref();

const speaker = new Room();
const listener = new Room();
let frames = 0;
let gotTrack = false;

listener.on(RoomEvent.TrackSubscribed, async (track) => {
  if (track.kind !== TrackKind.KIND_AUDIO) return;
  gotTrack = true;
  const { AudioStream } = await import('@livekit/rtc-node');
  const stream = new AudioStream(track);
  for await (const f of stream) {
    if (f.data.some((v) => v !== 0)) frames++;
    if (frames > 20) break;
  }
});

await speaker.connect(url, speakerTok, { autoSubscribe: true });
await listener.connect(url, listenerTok, { autoSubscribe: true });

const rate = 48000;
const source = new AudioSource(rate, 1);
const track = LocalAudioTrack.createAudioTrack('mic', source);
const opts = new TrackPublishOptions();
opts.source = TrackSource.SOURCE_MICROPHONE;
await speaker.localParticipant.publishTrack(track, opts);

const tone = () => {
  const n = rate / 100;
  const d = new Int16Array(n);
  for (let i = 0; i < n; i++) d[i] = Math.round(8000 * Math.sin((2 * Math.PI * 440 * i) / rate));
  return new AudioFrame(d, rate, 1, n);
};
for (let i = 0; i < 300 && frames <= 20; i++) {
  await source.captureFrame(tone());
}
await new Promise((r) => setTimeout(r, 1500));
if (!gotTrack) fail('listener never subscribed to the speaker track');
if (frames <= 20) fail('listener received no audible frames (' + frames + ')');

// A listener token must not be able to publish.
const lsrc = new AudioSource(rate, 1);
const ltrack = LocalAudioTrack.createAudioTrack('mic2', lsrc);
let refused = false;
try {
  await listener.localParticipant.publishTrack(ltrack, opts);
} catch {
  refused = true;
}
if (!refused) fail('a listener was allowed to publish');
console.log('OK: audio flowed (' + frames + ' frames) and listener publishing was refused');
await speaker.disconnect();
await listener.disconnect();
process.exit(0);
