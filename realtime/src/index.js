'use strict';
const { createRealtime } = require('./server');

for (const f of ['.env', '../.env']) {
  try { process.loadEnvFile(f); } catch { /* optional */ }
}

createRealtime({
  apiBase: process.env.API_BASE || `http://localhost:${process.env.PORT || 8080}`,
  jwtSecret: process.env.JWT_SECRET || '',
  port: Number(process.env.REALTIME_PORT || 3001),
  redisUrl: process.env.REDIS_URL || '',
  allowedOrigins: (process.env.APP_ALLOWED_ORIGINS || '').split(',').map((s) => s.trim()).filter(Boolean),
}).then((rt) => {
  console.log(`realtime listening on :${rt.port}`);
  const stop = () => rt.close().finally(() => process.exit(0));
  process.on('SIGINT', stop);
  process.on('SIGTERM', stop);
}).catch((err) => {
  console.error('fatal:', err.message);
  process.exit(1);
});
