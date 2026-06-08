const ua = process.env.npm_config_user_agent ?? '';
const execpath = process.env.npm_execpath ?? '';

const isBun = ua.startsWith('bun') || execpath.includes('bun');

if (!isBun) {
  const detected = ua.split('/')[0] || 'npm/yarn';
  process.stderr.write(`\n\x1b[31m✗ This project uses bun. Detected: ${detected}\x1b[0m\n`);
  process.stderr.write('  Install bun: https://bun.sh\n');
  process.stderr.write('  Then run: \x1b[36mbun install\x1b[0m\n\n');
  process.exit(1);
}
