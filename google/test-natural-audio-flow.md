# 自然音频流方案测试

## 问题根本原因分析

通过分析Deepgram和Asterisk的`sound:longsilence`机制，发现了真正的问题：

### 1. Deepgram的工作方式 ✅
- **Asterisk播放`sound:longsilence`**：保持RTP流持续活跃
- **Deepgram持续接收音频**：`udpproxy.StreamTo(dgClient)`发送所有RTP数据
- **自然处理**：包括静默音频在内的所有音频都正常处理

### 2. Google STT的问题 ❌
- **我们试图用静默模式模拟**：破坏了Google STT的连接状态
- **静默模式禁用后**：连接状态异常，导致音频超时
- **根本问题**：不应该替换真实音频流，而应该像Deepgram一样持续处理

## 解决方案：自然音频流

### 核心思想
**完全移除静默模式，让Google STT像Deepgram一样持续处理真实音频流**

### 关键变化

#### 1. 移除静默模式机制
- 删除`EnableSilentMode()`和`DisableSilentMode()`方法
- 删除`silentMode`字段和相关逻辑
- 删除静默音频包发送逻辑

#### 2. 恢复自然音频流处理
- `Write()`方法始终发送真实音频数据
- 心跳机制仅在中断模式下发送静默包
- 依赖Asterisk的`sound:longsilence`保持RTP流活跃

#### 3. 简化状态管理
- 只保留暂停/恢复机制用于中断检测
- 移除复杂的静默模式状态转换
- 让Google STT自然处理所有音频

## 修复后的工作流程

```
用户说话 → STT识别 → 翻译 → TTS播放
    ↓
Asterisk播放sound:longsilence → 保持RTP流活跃
    ↓
Google STT持续接收音频 → 自然处理静默和语音
    ↓
TTS播放完成 → 正常处理后续用户语音
```

## 代码变更详情

### voicebot.go
```go
// 移除静默模式启用
// No need for silent mode - let Google STT handle audio naturally like Deepgram
// The sound:longsilence from Asterisk will keep the RTP stream active

// 移除静默模式禁用
// No need to disable silent mode - we're not using it anymore
```

### google-stt.go
```go
// 移除静默模式字段
// No longer using silent mode - let Asterisk sound:longsilence handle continuous audio

// 简化Write方法
// Always send real audio data to Google STT (like Deepgram approach)
// The sound:longsilence from Asterisk will provide continuous audio stream

// 简化心跳机制
// Only send heartbeat in interrupt mode to detect user speech during TTS
```

## 测试步骤

### 1. 基本功能测试
```bash
# 设置环境变量
export USE_GOOGLE_STT_TTS=true
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json
export STT_LANGUAGE=en-US
export TTS_LANGUAGE=zh-CN
export TRANSLATE_SOURCE_LANGUAGE=en-US
export TRANSLATE_TARGET_LANGUAGE=zh-CN

# 启动系统
go run main.go
```

### 2. 自然音频流测试
1. **开始通话**：用户说"Hello"
2. **观察日志**：
   ```
   STT:Write callid=xxx status=AudioSent dataLen=160
   STT:heartbeat Event=Skipped - NormalMode
   ```
3. **TTS播放期间**：
   ```
   STT:Write callid=xxx status=AudioSent dataLen=160
   STT:heartbeat Event=Skipped - NormalMode
   ```
4. **TTS播放完成**：
   ```
   TTS:SpeakText callid=xxx status=ManualPlaybackComplete
   BOT:HandlePlaybackComplete callid=xxx playbackid=translation
   ```

### 3. 连续对话测试
1. **第一句话**：用户说"Hello"
2. **等待翻译完成**：观察自然音频流处理
3. **第二句话**：用户说"How are you?"
4. **验证**：第二句话应该正常识别和翻译，没有音频超时错误

### 4. 长时间对话测试
1. **多轮对话**：进行5-10轮对话
2. **观察日志**：确认没有静默模式相关日志
3. **验证稳定性**：系统应该稳定运行，没有超时错误

## 预期结果

### 正常情况
- ✅ 没有静默模式启用/禁用日志
- ✅ 持续发送真实音频数据到Google STT
- ✅ 心跳在正常模式下跳过
- ✅ 没有音频超时错误
- ✅ 连续对话正常工作

### 日志输出示例
```
STT:Write callid=xxx status=AudioSent dataLen=160
STT:heartbeat Event=Skipped - NormalMode
STT:MessageResponse Text="Hello"
BOT:HandleTranslationResults callid=xxx translated=你好
TTS:SpeakText callid=xxx status=ManualPlaybackComplete playbackID=translation
BOT:HandlePlaybackComplete callid=xxx playbackid=translation
STT:Write callid=xxx status=AudioSent dataLen=160
STT:MessageResponse Text="How are you?"
# 后续对话正常进行，没有超时错误
```

## 技术原理

### Asterisk sound:longsilence
- **作用**：播放长静默音频，保持RTP流活跃
- **位置**：`ariman.go`中的`startSilence()`函数
- **机制**：循环播放静默音频，确保RTP连接不中断

### Google STT自然处理
- **优势**：能够自然处理静默和语音音频
- **机制**：持续接收音频流，自动识别语音和静默
- **结果**：不需要人工干预音频流状态

### 对比分析
| 方案 | Deepgram | Google STT (旧) | Google STT (新) |
|------|----------|-----------------|-----------------|
| 音频流 | 持续处理 | 静默模式替换 | 持续处理 |
| 连接状态 | 自然保持 | 人工管理 | 自然保持 |
| 复杂度 | 简单 | 复杂 | 简单 |
| 稳定性 | 高 | 低 | 高 |

## 故障排除

### 问题1：仍然出现音频超时
**原因**：Asterisk的sound:longsilence没有正常工作
**解决**：检查Asterisk配置，确保longsilence文件存在

### 问题2：连续对话不工作
**原因**：Google STT连接状态异常
**解决**：检查网络连接和Google Cloud配置

### 问题3：TTS播放期间无法中断
**原因**：中断检测机制没有正确配置
**解决**：检查暂停/恢复机制是否正常工作

## 总结

自然音频流方案的核心优势：

1. **简单可靠**：移除复杂的静默模式管理
2. **自然处理**：让Google STT像Deepgram一样处理音频
3. **稳定连接**：依赖Asterisk的sound:longsilence保持RTP活跃
4. **易于维护**：减少状态管理和错误处理复杂度

这个方案应该能彻底解决音频超时问题，提供与Deepgram相同的稳定性和可靠性。
