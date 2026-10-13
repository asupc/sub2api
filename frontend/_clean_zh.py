import io, re, sys

def load(p):
    return io.open(p, encoding='utf-8').read().splitlines(keepends=False)

def save(p, lines):
    io.open(p, 'w', encoding='utf-8', newline='').write('\n'.join(lines) + '\n')

def remove_block(lines, header, indent):
    """Remove `indent + header + ': {'` block through its matching `indent + '},'`."""
    start_pat = re.compile(r'^' + re.escape(indent) + re.escape(header) + r': \{')
    for i, ln in enumerate(lines):
        if start_pat.match(ln):
            depth = 1
            j = i + 1
            while j < len(lines) and depth > 0:
                depth += lines[j].count('{') - lines[j].count('}')
                if depth == 0:
                    break
                j += 1
            # lines[i+1..j] removed (keep closing line? closing belongs to block)
            return lines[:i] + lines[j+1:]
    raise SystemExit('block not found: ' + header + ' (indent=' + indent + ')')

def remove_leaves(lines, prefix_indent, names, prefix_header):
    """Remove leaf lines `prefix_indent + name: ...,` inside a given block scope.
    Scoped by finding the block `prefix_header: {` at indent level `block_indent`,
    then removing `leaf_indent + name: ...` lines inside it."""
    block_pat = re.compile(r'^' + re.escape(prefix_indent) + re.escape(prefix_header) + r': \{')
    start = None
    for i, ln in enumerate(lines):
        if block_pat.match(ln):
            start = i
            break
    if start is None:
        raise SystemExit('scope block not found: ' + prefix_header)
    leaf_indent = prefix_indent + '  '
    out = []
    inside = False
    depth_done = False
    for i, ln in enumerate(lines):
        if i == start:
            inside = True
            out.append(ln)
            continue
        if inside and not depth_done:
            if re.match(r'^' + re.escape(prefix_indent) + r'\}', ln):
                inside = False
                depth_done = True
                out.append(ln)
                continue
            m = re.match(r'^' + re.escape(leaf_indent) + r'([A-Za-z_0-9]+):', ln)
            if m and m.group(1) in names:
                continue
            out.append(ln)
            continue
        out.append(ln)
    if not depth_done:
        raise SystemExit('scope block never closed: ' + prefix_header)
    return out

# ============ zh/common.ts ============
p = 'src/i18n/locales/zh/common.ts'
lines = load(p)
# nav leaves to remove
nav_remove = {'announcements', 'redeem', 'availableChannels', 'modelPlaza', 'redeemCodes', 'promoCodes', 'mySubscriptions', 'subscriptions', 'channelManagement', 'channelPricing', 'channelStatus', 'channelMonitor', 'language'}
lines = remove_leaves(lines, '  ', nav_remove, 'nav')
# auth: remove whole sub-blocks
for sub in ['linuxdo', 'dingtalk', 'oidc', 'oauthFlow', 'oauth', 'emailOAuth']:
    lines = remove_block(lines, sub, '    ')  # nested inside auth (4-space)
# auth leaves
auth_remove = set()
for ln in lines:
    m = re.match(r'^    ([A-Za-z_0-9]+):', ln)
auth_leaves_re = re.compile(r'^    ([A-Za-z_0-9]+):')
# collect auth leaf names present
# simpler: explicitly listed ones
auth_remove = {'passkeySignIn','passkeySigningIn','passkeyCancelled','passkeyFailed','rememberMe','promoCodeLabel','promoCodePlaceholder','promoCodeValid','promoCodeInvalid','promoCodeNotFound','promoCodeExpired','promoCodeDisabled','promoCodeMaxUsed','promoCodeAlreadyUsed','promoCodeValidating','promoCodeInvalidCannotRegister','invitationCodeLabel','invitationCodePlaceholder','invitationCodeRequired','invitationCodeValid','invitationCodeInvalid','invitationCodeValidating','invitationCodeInvalidCannotRegister','oauthOrContinue','linuxdoCallbackPageTitle','dingtalkCallbackPageTitle','oidcCallbackPageTitle','oauthCallbackPageTitle','wechatProviderName','wechatCallbackPageTitle'}
out = []
inside_auth = False
for ln in lines:
    if re.match(r'^  auth: \{', ln):
        inside_auth = True
        out.append(ln); continue
    if inside_auth and re.match(r'^  \}', ln):
        inside_auth = False
        out.append(ln); continue
    if inside_auth:
        m = auth_leaves_re.match(ln)
        if m and m.group(1) in auth_remove:
            continue
    out.append(ln)
lines = out
# stepUp block (top-level 2-space)
lines = remove_block(lines, 'stepUp', '  ')
save(p, lines)

# ============ zh/dashboard.ts ============
p = 'src/i18n/locales/zh/dashboard.ts'
lines = load(p)
for blk in ['availableChannels', 'modelPlaza']:
    lines = remove_block(lines, blk, '  ')
# redeem block: keep only used leaves
keep_redeem = {'adminAdjustment','balanceAddedRedeem','balanceAddedAffiliate','balanceAddedAdmin','balanceDeductedAdmin','concurrencyAddedRedeem','concurrencyAddedAdmin','concurrencyReducedAdmin','subscriptionAssigned'}
block_pat = re.compile(r'^  redeem: \{')
start = None
for i, ln in enumerate(lines):
    if block_pat.match(ln):
        start = i; break
out = []
inside = False
for i, ln in enumerate(lines):
    if i == start:
        inside = True; out.append(ln); continue
    if inside and re.match(r'^  \}', ln):
        inside = False; out.append(ln); continue
    if inside:
        m = re.match(r'^    ([A-Za-z_0-9]+):', ln)
        if m and m.group(1) not in keep_redeem:
            continue
    out.append(ln)
lines = out
save(p, lines)

# ============ zh/misc.ts ============
p = 'src/i18n/locales/zh/misc.ts'
lines = load(p)
for blk in ['subscriptionProgress', 'announcements', 'userSubscriptions']:
    lines = remove_block(lines, blk, '  ')
save(p, lines)

# ============ zh/admin/channels.ts (4-space indent) ============
p = 'src/i18n/locales/zh/admin/channels.ts'
lines = load(p)
lines = remove_block(lines, 'availableChannels', '    ')
# channels: delete unused leaves under `channels: {` except dynamic-validation subtrees
unused_channels = set(open('/tmp/unused_channels.txt', encoding='utf-8').read().split()) if False else None
save(p, lines)

# ============ zh/admin/resources.ts ============
p = 'src/i18n/locales/zh/admin/resources.ts'
lines = load(p)
for blk in ['announcements', 'promo', 'subscriptions']:
    lines = remove_block(lines, blk, '    ')
# redeem: keep only userPrefix
keep = {'userPrefix'}
start = None
for i, ln in enumerate(lines):
    if re.match(r'^    redeem: \{', ln):
        start = i; break
out = []
inside = False
for i, ln in enumerate(lines):
    if i == start:
        inside = True; out.append(ln); continue
    if inside and re.match(r'^    \}', ln):
        inside = False; out.append(ln); continue
    if inside:
        m = re.match(r'^      ([A-Za-z_0-9]+):', ln)
        if m and m.group(1) not in keep:
            continue
    out.append(ln)
lines = out
save(p, lines)

# ============ zh/admin/settings.ts ============
p = 'src/i18n/locales/zh/admin/settings.ts'
lines = load(p)
# features.availableChannels & features.modelPlaza (8-space indent under settings.features)
for blk in ['availableChannels', 'modelPlaza']:
    lines = remove_block(lines, blk, '        ')
save(p, lines)

print('OK')
