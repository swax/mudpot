<?php
$logFile = '/var/log/mudpot.log';
$lines = file_exists($logFile) ? file($logFile, FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES) : [];

// Parse sessions
$sessions = [];
$current = [];

foreach ($lines as $line) {
    if (!preg_match('/^(\S+) \[(.+?)\] (.+)$/', $line, $m)) continue;
    $ts = $m[1];
    $ip = $m[2];
    $msg = $m[3];

    if ($ip === 'SYSTEM') continue;
    if ($ip === '::1') continue; // skip localhost test sessions

    if ($msg === 'CONNECTED') {
        $current[$ip] = [
            'ip' => $ip,
            'start' => $ts,
            'end' => $ts,
            'commands' => [],
            'rooms' => ['lobby'],
            'items' => [],
            'furthest' => 'lobby',
            'reached_vault' => false,
            'kicked' => false,
            'kick_reason' => null,
        ];
    } elseif (isset($current[$ip])) {
        $current[$ip]['end'] = $ts;

        if (preg_match('/^\[(.+?)\] (.+)$/', $msg, $cm)) {
            $room = $cm[1];
            $cmd = $cm[2];
            $current[$ip]['commands'][] = ['room' => $room, 'cmd' => $cmd, 'ts' => $ts];
        }
        if (preg_match('/^MOVED to (.+)$/', $msg, $mm)) {
            $room = $mm[1];
            $current[$ip]['rooms'][] = $room;
            $current[$ip]['furthest'] = $room;
            if ($room === 'vault') $current[$ip]['reached_vault'] = true;
        }
        if (preg_match('/^TOOK (.+)$/', $msg, $tm)) {
            $current[$ip]['items'][] = $tm[1];
        }
        if (preg_match('/^KICKED: (.+)$/', $msg, $km)) {
            $current[$ip]['kicked'] = true;
            $current[$ip]['kick_reason'] = $km[1];
        }
        if (strpos($msg, 'DISCONNECTED') === 0 || $msg === 'QUIT' || $msg === 'TIMEOUT') {
            $sess = $current[$ip];
            $start = strtotime($sess['start']);
            $end = strtotime($sess['end']);
            $sess['duration'] = max($end - $start, 0);
            $sess['cmd_count'] = count($sess['commands']);
            $sess['room_count'] = count(array_unique($sess['rooms']));
            $sessions[] = $sess;
            unset($current[$ip]);
        }
    }
}

// Stats
$totalSessions = count($sessions);
$uniqueIPs = count(array_unique(array_column($sessions, 'ip')));
$totalCmds = array_sum(array_column($sessions, 'cmd_count'));
$totalTime = array_sum(array_column($sessions, 'duration'));
$vaultReached = count(array_filter($sessions, function($s) { return $s['reached_vault']; }));
$kicked = count(array_filter($sessions, function($s) { return $s['kicked']; }));

// Room depth ordering
$roomDepth = [
    'lobby' => 0, 'storage' => 1, 'corridor' => 2, 'archive' => 3,
    'lab' => 4, 'vault_door' => 5, 'vault' => 6
];

// Room visit counts
$roomVisits = [];
foreach ($sessions as $s) {
    foreach (array_unique($s['rooms']) as $r) {
        $roomVisits[$r] = ($roomVisits[$r] ?? 0) + 1;
    }
}

// Furthest room distribution
$furthestDist = [];
foreach ($sessions as $s) {
    $r = $s['furthest'];
    $furthestDist[$r] = ($furthestDist[$r] ?? 0) + 1;
}

// Most common commands
$allCmds = [];
foreach ($sessions as $s) {
    foreach ($s['commands'] as $c) {
        $cmd = strtolower(trim($c['cmd']));
        $allCmds[$cmd] = ($allCmds[$cmd] ?? 0) + 1;
    }
}
arsort($allCmds);
$topCmds = array_slice($allCmds, 0, 15, true);

// Recent sessions (last 20)
$recentSessions = array_slice(array_reverse($sessions), 0, 20);

// Room display names
$roomNames = [
    'lobby' => 'Lobby', 'storage' => 'Storage', 'corridor' => 'Corridor',
    'archive' => 'Archive', 'lab' => 'Lab', 'vault_door' => 'Vault Door', 'vault' => 'Vault'
];

function formatDuration($secs) {
    if ($secs < 60) return $secs . 's';
    if ($secs < 3600) return floor($secs/60) . 'm ' . ($secs%60) . 's';
    return floor($secs/3600) . 'h ' . floor(($secs%3600)/60) . 'm';
}

function depthBar($room, $roomDepth) {
    $depth = $roomDepth[$room] ?? 0;
    $maxDepth = 6;
    $filled = $depth + 1;
    return str_repeat('█', $filled) . str_repeat('░', $maxDepth - $depth);
}

function escHtml($s) { return htmlspecialchars($s, ENT_QUOTES, 'UTF-8'); }
?>
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MudPot // Honeypot Dashboard</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'Courier New', monospace;
            background: #0a0a0a;
            color: #ccc;
            line-height: 1.6;
            min-height: 100vh;
            padding: 40px;
        }
        .container { max-width: 1200px; margin: 0 auto; }
        header {
            margin-bottom: 40px;
            padding-bottom: 20px;
            border-bottom: 1px solid #333;
        }
        h1 { font-size: 1.8em; color: #fff; margin-bottom: 4px; }
        .subtitle { color: #666; font-size: 0.9em; }
        h2 {
            font-size: 1.1em;
            color: #fff;
            margin: 30px 0 15px 0;
            padding-bottom: 5px;
            border-bottom: 1px solid #333;
        }

        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
            gap: 15px;
            margin-bottom: 10px;
        }
        .stat-box {
            background: #111;
            border: 1px solid #222;
            padding: 15px;
        }
        .stat-value {
            font-size: 2em;
            color: #fff;
            font-weight: bold;
        }
        .stat-value.green { color: #4a4; }
        .stat-value.red { color: #a44; }
        .stat-value.yellow { color: #aa4; }
        .stat-label { color: #666; font-size: 0.85em; }

        table {
            width: 100%;
            border-collapse: collapse;
            font-size: 0.9em;
        }
        th {
            text-align: left;
            color: #888;
            padding: 8px 12px;
            border-bottom: 1px solid #333;
            font-weight: normal;
        }
        td {
            padding: 6px 12px;
            border-bottom: 1px solid #1a1a1a;
        }
        tr:hover { background: #111; }

        .bar { color: #4a4; letter-spacing: -1px; }
        .bar-label { color: #888; min-width: 100px; display: inline-block; }
        .bar-count { color: #666; margin-left: 8px; }

        .cmd { color: #aaa; }
        .ip { color: #6a6; }
        .room { color: #aa6; }
        .vault { color: #4a4; font-weight: bold; }
        .kicked-tag { color: #a44; }
        .timeout-tag { color: #a84; }

        .bar-row {
            margin: 4px 0;
            display: flex;
            align-items: center;
            gap: 8px;
        }
        .bar-fill {
            background: #2a4a2a;
            height: 18px;
            min-width: 2px;
            display: flex;
            align-items: center;
            padding: 0 6px;
            font-size: 0.8em;
            color: #6a6;
        }

        .maze-map {
            font-size: 0.85em;
            line-height: 1.4;
            color: #555;
            margin: 15px 0;
            padding: 15px;
            background: #0d0d0d;
            border: 1px solid #1a1a1a;
            white-space: pre;
        }
        .maze-map .visited { color: #4a4; }
        .maze-map .hot { color: #a44; }

        .session-cmds {
            display: none;
            padding: 10px 12px;
            background: #0d0d0d;
            border-bottom: 1px solid #1a1a1a;
            font-size: 0.85em;
            color: #888;
        }
        .session-cmds.open { display: table-row; }
        .session-cmds td { padding: 10px 12px; }
        .cmd-line { margin: 2px 0; }
        .cmd-room { color: #aa6; }
        .cmd-text { color: #ccc; }
        .toggle { cursor: pointer; color: #666; }
        .toggle:hover { color: #fff; }

        footer {
            margin-top: 60px;
            padding-top: 20px;
            border-top: 1px solid #333;
            text-align: center;
            color: #444;
            font-size: 0.85em;
        }
        footer a { color: #666; text-decoration: none; }
        footer a:hover { color: #fff; }

        @media (max-width: 768px) {
            body { padding: 15px; }
            .stats-grid { grid-template-columns: repeat(2, 1fr); }
            table { font-size: 0.8em; }
            th, td { padding: 4px 6px; }
        }
    </style>
</head>
<body>
<div class="container">
    <header>
        <h1>$ mudpot --status</h1>
        <span class="subtitle">honeypot telnet MUD // trapping scanners since 2026</span>
    </header>

    <h2>Overview</h2>
    <div class="stats-grid">
        <div class="stat-box">
            <div class="stat-value"><?= $totalSessions ?></div>
            <div class="stat-label">total sessions</div>
        </div>
        <div class="stat-box">
            <div class="stat-value"><?= $uniqueIPs ?></div>
            <div class="stat-label">unique visitors</div>
        </div>
        <div class="stat-box">
            <div class="stat-value"><?= $totalCmds ?></div>
            <div class="stat-label">commands entered</div>
        </div>
        <div class="stat-box">
            <div class="stat-value"><?= formatDuration($totalTime) ?></div>
            <div class="stat-label">total time wasted</div>
        </div>
        <div class="stat-box">
            <div class="stat-value green"><?= $vaultReached ?></div>
            <div class="stat-label">reached the vault</div>
        </div>
        <div class="stat-box">
            <div class="stat-value red"><?= $kicked ?></div>
            <div class="stat-label">kicked (abuse)</div>
        </div>
    </div>

    <h2>Maze Progress</h2>
    <p style="color:#666; margin-bottom:10px;">How far visitors got before disconnecting</p>
    <?php
    $orderedRooms = ['lobby','storage','corridor','archive','lab','vault_door','vault'];
    $maxCount = max(array_values($furthestDist) ?: [1]);
    foreach ($orderedRooms as $r):
        $count = $furthestDist[$r] ?? 0;
        $width = $maxCount > 0 ? max(($count / $maxCount) * 100, 1) : 1;
        $name = $roomNames[$r] ?? $r;
    ?>
    <div class="bar-row">
        <span class="bar-label"><?= $name ?></span>
        <div class="bar-fill" style="width:<?= $width ?>%"><?= $count ?></div>
    </div>
    <?php endforeach; ?>

    <h2>Room Traffic</h2>
    <p style="color:#666; margin-bottom:10px;">Total unique sessions that visited each room</p>
    <?php
    $maxVisit = max(array_values($roomVisits) ?: [1]);
    foreach ($orderedRooms as $r):
        $count = $roomVisits[$r] ?? 0;
        $width = $maxVisit > 0 ? max(($count / $maxVisit) * 100, 1) : 1;
        $name = $roomNames[$r] ?? $r;
    ?>
    <div class="bar-row">
        <span class="bar-label"><?= $name ?></span>
        <div class="bar-fill" style="width:<?= $width ?>%"><?= $count ?></div>
    </div>
    <?php endforeach; ?>

    <h2>Top Commands</h2>
    <table>
        <tr><th>command</th><th>count</th></tr>
        <?php foreach ($topCmds as $cmd => $count): ?>
        <tr>
            <td class="cmd"><?= escHtml($cmd) ?></td>
            <td><?= $count ?></td>
        </tr>
        <?php endforeach; ?>
    </table>

    <h2>Recent Sessions</h2>
    <table>
        <tr><th>time</th><th>ip</th><th>duration</th><th>cmds</th><th>furthest</th><th>status</th><th></th></tr>
        <?php foreach ($recentSessions as $i => $s): ?>
        <tr onclick="toggleCmds(<?= $i ?>)" class="toggle">
            <td><?= date('M j H:i', strtotime($s['start'])) ?></td>
            <td class="ip"><?= escHtml($s['ip']) ?></td>
            <td><?= formatDuration($s['duration']) ?></td>
            <td><?= $s['cmd_count'] ?></td>
            <td class="<?= $s['reached_vault'] ? 'vault' : 'room' ?>"><?= $roomNames[$s['furthest']] ?? $s['furthest'] ?></td>
            <td>
                <?php if ($s['reached_vault']): ?><span class="vault">completed</span>
                <?php elseif ($s['kicked']): ?><span class="kicked-tag">kicked: <?= escHtml($s['kick_reason']) ?></span>
                <?php else: ?>-
                <?php endif; ?>
            </td>
            <td class="toggle">[+]</td>
        </tr>
        <tr class="session-cmds" id="cmds-<?= $i ?>">
            <td colspan="7">
                <?php foreach ($s['commands'] as $c): ?>
                <div class="cmd-line">
                    <span class="cmd-room">[<?= escHtml($c['room']) ?>]</span>
                    <span class="cmd-text"><?= escHtml($c['cmd']) ?></span>
                </div>
                <?php endforeach; ?>
                <?php if (empty($s['commands'])): ?>
                <span style="color:#555">no commands entered</span>
                <?php endif; ?>
            </td>
        </tr>
        <?php endforeach; ?>
    </table>

    <footer>
        <p>mudpot // <a href="https://transparentsource.org">transparentsource.org</a></p>
    </footer>
</div>

<script>
function toggleCmds(i) {
    var el = document.getElementById('cmds-' + i);
    el.classList.toggle('open');
}
</script>
</body>
</html>
