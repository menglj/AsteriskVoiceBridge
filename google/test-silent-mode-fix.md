# 静默模式修复测试

## 问题分析

从实际运行日志分析，发现了以下问题：

1. **静默模式启用了**：`STT:EnableSilentMode callid=xxx status=SilentModeEnabled`
2. **但没有看到静默音频包发送日志**：应该看到 `STT:Write callid=xxx status=SilentMode - SendingSilentAudio`
3. **TTS播放完成后静默模式没有被禁用**：没有看到 `STT:DisableSilentMode` 日志
4. **仍然出现音频超时错误**：说明静默音频包没有正确发送

## 修复方案

### 1. 增强日志记录
- 将静默模式的日志级别从 `Debug` 改为 `Info`
- 添加详细的静默音频包发送日志
- 添加TTS播放完成回调的调试日志

### 2. 备用定时器机制
- 在启用静默模式时，设置10秒定时器
- 定时器到期后自动禁用静默模式
- 作为TTS播放完成回调的备用机制

### 3. 调试TTS播放完成
- 添加 `currentPlaybackID` 的跟踪日志
- 添加播放完成回调的详细日志
- 确保播放完成回调被正确调用

## 修复后的代码变更

### Google STT (google-stt.go)
```go
// 增强静默模式日志
if c.IsSilentMode() {
    log.Info("STT:Write", "callid", c.callid, "status", "SilentMode - SendingSilentAudio", "dataLen", len(data))
    // ... 发送静默音频包
    log.Info("STT:Write", "callid", c.callid, "status", "SilentAudioSent", "silentLen", len(silentPacket))
}

// 增强心跳日志
log.Info("STT:heartbeat", "Event", "SilentPacketSent")
```

### Google TTS (google-tts.go)
```go
// 添加播放完成回调调试
func (tts *ttsCall) playbackCompleteCallBack() {
    log.Info("TTS:playbackCompleteCallBack", "callid", tts.callid, "currentPlaybackID", tts.currentPlaybackID, "hasCallback", tts.playbackCompleteCB != nil)
    if tts.playbackCompleteCB != nil && tts.currentPlaybackID != "" {
        log.Info("TTS:playbackCompleteCallBack", "callid", tts.callid, "status", "CallingCallback", "playbackID", tts.currentPlaybackID)
        tts.playbackCompleteCB(tts.callid, tts.currentPlaybackID)
        tts.currentPlaybackID = ""
    }
}

// 添加播放ID设置日志
call.currentPlaybackID = id
log.Info("TTS:SendText", "callid", callid, "playbackID", id, "status", "SetPlaybackID")
```

### VoiceBot (voicebot.go)
```go
// 添加备用定时器机制
if v.googleSTTProvider != nil {
    v.googleSTTProvider.EnableSilentMode(callid)
    // Set a timer to disable silent mode after TTS playback (fallback mechanism)
    go func() {
        time.Sleep(10 * time.Second) // Wait 10 seconds for TTS to complete
        if v.googleSTTProvider != nil {
            v.googleSTTProvider.DisableSilentMode(callid)
            log.Info("BOT:HandleTranslationResults", "callid", callid, "status", "SilentModeDisabledByTimer")
        }
    }()
}
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

### 2. 静默模式测试
1. **开始通话**：用户说"Hello"
2. **观察日志**：
   ```
   STT:EnableSilentMode callid=xxx status=SilentModeEnabled
   TTS:SendText callid=xxx playbackID=translation status=SetPlaybackID
   ```
3. **TTS播放期间**：观察静默音频包发送
   ```
   STT:Write callid=xxx status=SilentMode - SendingSilentAudio dataLen=xxx
   STT:Write callid=xxx status=SilentAudioSent silentLen=xxx
   STT:heartbeat Event=SilentPacketSent
   ```

### 3. 播放完成测试
1. **TTS播放完成**：观察播放完成回调
   ```
   TTS:playbackCompleteCallBack callid=xxx currentPlaybackID=translation hasCallback=true
   TTS:playbackCompleteCallBack callid=xxx status=CallingCallback playbackID=translation
   BOT:HandlePlaybackComplete callid=xxx playbackid=translation
   STT:DisableSilentMode callid=xxx status=SilentModeDisabled
   ```

### 4. 备用定时器测试
1. **如果播放完成回调失败**：观察定时器机制
   ```
   BOT:HandleTranslationResults callid=xxx status=SilentModeDisabledByTimer
   STT:DisableSilentMode callid=xxx status=SilentModeDisabled
   ```

### 5. 中断检测测试
1. **TTS播放期间**：用户说"Stop"
2. **观察日志**：
   ```
   STT:InterruptDetection callid=xxx text=Stop
   BOT:HandleTranscriptResults callid=xxx status=InterruptDetected text=Stop
   STT:DisableSilentMode callid=xxx status=SilentModeDisabled
   ```

## 预期结果

### 正常情况
- ✅ 静默模式正确启用和禁用
- ✅ 静默音频包持续发送
- ✅ 没有音频超时错误
- ✅ 用户中断正常工作
- ✅ 播放完成回调正确触发

### 日志输出示例
```
STT:EnableSilentMode callid=xxx status=SilentModeEnabled
TTS:SendText callid=xxx playbackID=translation status=SetPlaybackID
STT:Write callid=xxx status=SilentMode - SendingSilentAudio dataLen=160
STT:Write callid=xxx status=SilentAudioSent silentLen=160
STT:heartbeat Event=SilentPacketSent
TTS:playbackCompleteCallBack callid=xxx currentPlaybackID=translation hasCallback=true
TTS:playbackCompleteCallBack callid=xxx status=CallingCallback playbackID=translation
BOT:HandlePlaybackComplete callid=xxx playbackid=translation
STT:DisableSilentMode callid=xxx status=SilentModeDisabled
```

## 故障排除

### 问题1：仍然出现音频超时
**原因**：静默音频包没有正确发送
**解决**：检查 `STT:Write` 日志，确认静默音频包被发送

### 问题2：静默模式没有被禁用
**原因**：播放完成回调没有触发
**解决**：检查 `TTS:playbackCompleteCallBack` 日志，确认回调被调用

### 问题3：定时器机制触发
**原因**：播放完成回调失败
**解决**：检查 `BOT:HandleTranslationResults status=SilentModeDisabledByTimer` 日志

## 总结

修复后的静默模式方案包含：
1. **增强的日志记录**：便于调试和监控
2. **备用定时器机制**：确保静默模式能够被禁用
3. **详细的回调跟踪**：确保TTS播放完成被正确处理

这个修复方案应该能解决音频超时问题，并提供稳定的静默模式功能。
