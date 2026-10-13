# Third-Party Notices / 第三方组件声明

本文件列出本仓库中包含的、**不由本仓库以 LGPL-3.0 授权**的第三方来源代码及其适用条款。

This file lists third-party source code included in this repository that is **NOT licensed
under this repository's LGPL-3.0 terms**, together with the terms that do apply.

> ⚠️ **重要**：本仓库根目录的 `LICENSE`（GNU LGPL-3.0）**不覆盖**下文列出的任何部分。
> 这些部分仅按其各自的原始条款提供。
>
> **IMPORTANT**: The `LICENSE` file (GNU LGPL-3.0) at the repository root **does NOT apply**
> to any of the components listed below. Those components are provided solely under their
> own original terms, as reproduced or referenced here.

---

## 1. trae-workbuddy-switch — PolyForm Noncommercial License 1.0.0

### 1.1 Required Notice（原样保留，不得删除）

```
Required Notice: Copyright (c) 2026 NextAgentX (https://github.com/NextAgentX/trae-workbuddy-switch)
```

### 1.2 适用条款 / Applicable Terms

本仓库中源自 [trae-workbuddy-switch](https://github.com/NextAgentX/trae-workbuddy-switch)
的代码，适用 **PolyForm Noncommercial License 1.0.0**。
许可全文见 <https://polyformproject.org/licenses/noncommercial/1.0.0>，
并完整转载于本文件第 1.4 节。

The code in this repository derived from
[trae-workbuddy-switch](https://github.com/NextAgentX/trae-workbuddy-switch) is licensed
under the **PolyForm Noncommercial License 1.0.0**. The full text is available at
<https://polyformproject.org/licenses/noncommercial/1.0.0> and reproduced in Section 1.4 below.

### 1.3 涉及的文件 / Affected Files

以下文件为 `trae-workbuddy-switch`（Rust）项目中
`crates/buddy-switch-core/src/modules/trae/` 与 `crates/buddy-switch-gateway/src/trae/`
的 Go 语言移植，受 PolyForm Noncommercial 1.0.0 约束：

**Go 后端**

- `backend/internal/pkg/trae/`（全部文件：`checkin.go`、`credentials.go`、`jwt.go`、
  `models.go`、`oauth.go`、`payload.go`、`refresh.go`、`sse.go`、`usage.go`、`variant.go`
  及其对应 `*_test.go`）
- `backend/internal/handler/admin/trae_oauth_handler.go`
- `backend/internal/service/openai_gateway_trae.go`（及其 `_test.go`）
- `backend/internal/service/trae_oauth_service.go`（及其 `_test.go`）
- `backend/internal/service/trae_ops_service.go`（及其 `_test.go`）

**前端**

- `frontend/src/components/account/forms/TraeForm.vue`

> 注：上述文件与其他平台（WorkBuddy / Qoder）共用的基础设施文件，如
> `backend/internal/service/platform_ops.go`，其移植来源见第 2、3 节。

### 1.4 许可全文 / Full License Text

```
Required Notice: Copyright (c) 2026 NextAgentX (https://github.com/NextAgentX/trae-workbuddy-switch)

# PolyForm Noncommercial License 1.0.0

<https://polyformproject.org/licenses/noncommercial/1.0.0>

## Acceptance

In order to get any license under these terms, you must agree to them as both strict obligations and conditions to all your licenses.

## Copyright License

The licensor grants you a copyright license for the software to do everything you might do with the software that would otherwise infringe the licensor's copyright in it for any permitted purpose.  However, you may only distribute the software according to [Distribution License](#distribution-license) and make changes or new works based on the software according to [Changes and New Works License](#changes-and-new-works-license).

## Distribution License

The licensor grants you an additional copyright license to distribute copies of the software.  Your license to distribute covers distributing the software with changes and new works permitted by [Changes and New Works License](#changes-and-new-works-license).

## Notices

You must ensure that anyone who gets a copy of any part of the software from you also gets a copy of these terms or the URL for them above, as well as copies of any plain-text lines beginning with `Required Notice:` that the licensor provided with the software.  For example:

> Required Notice: Copyright Yoyodyne, Inc. (http://example.com)

## Changes and New Works License

The licensor grants you an additional copyright license to make changes and new works based on the software for any permitted purpose.

## Patent License

The licensor grants you a patent license for the software that covers patent claims the licensor can license, or becomes able to license, that you would infringe by using the software.

## Noncommercial Purposes

Any noncommercial purpose is a permitted purpose.

## Personal Uses

Personal use for research, experiment, and testing for the benefit of public knowledge, personal study, private entertainment, hobby projects, amateur pursuits, or religious observance, without any anticipated commercial application, is use for a permitted purpose.

## Noncommercial Organizations

Use by any charitable organization, educational institution, public research organization, public safety or health organization, environmental protection organization, or government institution is use for a permitted purpose regardless of the source of funding or obligations resulting from the funding.

## Fair Use

You may have "fair use" rights for the software under the law. These terms do not limit them.

## No Other Rights

These terms do not allow you to sublicense or transfer any of your licenses to anyone else, or prevent the licensor from granting licenses to anyone else.  These terms do not imply any other licenses.

## Patent Defense

If you make any written claim that the software infringes or contributes to infringement of any patent, your patent license for the software granted under these terms ends immediately. If your company makes such a claim, your patent license ends immediately for work on behalf of your company.

## Violations

The first time you are notified in writing that you have violated any of these terms, or done anything with the software not covered by your licenses, your licenses can nonetheless continue if you come into full compliance with these terms, and take practical steps to correct past violations, within 32 days of receiving notice.  Otherwise, all your licenses end immediately.

## No Liability

***As far as the law allows, the software comes as is, without any warranty or condition, and the licensor will not be liable to you for any damages arising out of these terms or the use or nature of the software, under any kind of legal claim.***

## Definitions

The **licensor** is the individual or entity offering these terms, and the **software** is the software the licensor makes available under these terms.

**You** refers to the individual or entity agreeing to these terms.

**Your company** is any legal entity, sole proprietorship, or other kind of organization that you work for, plus all organizations that have control over, are under the control of, or are under common control with that organization.  **Control** means ownership of substantially all the assets of an entity, or the power to direct its management and policies by vote, contract, or otherwise.  Control can be direct or indirect.

**Your licenses** are all the licenses granted to you for the software under these terms.

**Use** means anything you do with the software requiring one of your licenses.
```

---

## 2. workbuddy2api-panel — MIT License

本仓库中源自 [workbuddy2api-panel](https://github.com/linguo2625469/workbuddy2api-panel)
的代码，适用 **MIT License**。涉及文件包括但不限于：

- `backend/internal/pkg/workbuddy/`（全部文件）
- `backend/internal/service/openai_gateway_workbuddy.go`
- `backend/internal/service/platform_ops.go`（三平台共用基建）

```
MIT License

Copyright (c) 2026 Sliverkiss (original project: https://github.com/Sliverkiss/workbuddy2api)
Copyright (c) 2026 linguo2625469 (this fork: https://github.com/linguo2625469/workbuddy2api-panel)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## 3. qoder2api-hub — MIT License

本仓库中源自 [qoder2api-hub](https://github.com/shuishuipingan/qoder2api-hub)
的代码，适用 **MIT License**。涉及文件包括但不限于：

- `backend/internal/pkg/qoder/`（全部文件，含 `assets/` 下的目录快照 JSON）
- `backend/internal/service/openai_gateway_qoder.go`
- `backend/internal/service/qoder_ops_service.go`

来源：Python 项目 `qoder_sign.py`（COSY 签名）、`qoder_proxy.py`（网关主体/协议转换）、
`qoder_accounts.py`（账号池/OAuth/token 刷新）、`baseprompt.json`（上游请求体骨架）、
`qoder_catalog_{intl,cn}.json`（模型快照）的 Go 移植。

```
MIT License

Copyright (c) 2026

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## 4. 其余部分 / Remainder

除上述第 1–3 节列明的部分外，本仓库其余内容按根目录 `LICENSE`
（GNU Lesser General Public License v3.0）授权，原始版权归
`Copyright (c) 2026 Wesley Liddick`（上游项目
[Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api)）及本项目二开贡献者所有。

Except for the components listed in Sections 1–3 above, the remainder of this repository
is licensed under the GNU Lesser General Public License v3.0 as stated in the root
`LICENSE` file, with original copyright held by
`Copyright (c) 2026 Wesley Liddick` (upstream project
[Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api)) and the contributors of this fork.
