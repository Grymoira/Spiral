#pragma once

#include <QtGlobal>

// Detects a discontinuity between the local wall clock and Qt's monotonic
// clock. Some platform monotonic clocks do not advance during system sleep.
// This is deliberately a fail-closed hint, NOT an alternative time source
// for scheduled playback or an acoustic synchronization guarantee.
class ClockContinuityGuard final {
public:
    static constexpr qint64 MaxDriftMs = 1500;

    bool observe(qint64 monotonicMs, qint64 wallMs) {
        if (monotonicMs < 0 || wallMs < 0) {
            initialized_ = false;
            return false;
        }
        if (!initialized_) {
            lastMonotonicMs_ = monotonicMs;
            lastWallMs_ = wallMs;
            initialized_ = true;
            return true;
        }
        const qint64 monotonicDelta = monotonicMs - lastMonotonicMs_;
        const qint64 wallDelta = wallMs - lastWallMs_;
        lastMonotonicMs_ = monotonicMs;
        lastWallMs_ = wallMs;

        // Both sources progressing together is normal, including a long gap
        // when the platform's monotonic clock counts sleep. Existing server
        // freshness and late-execution guards cover that case.
        if (monotonicDelta < 0 || wallDelta < 0)
            return false;
        const qint64 drift = wallDelta - monotonicDelta;
        return drift >= -MaxDriftMs && drift <= MaxDriftMs;
    }

private:
    bool initialized_ = false;
    qint64 lastMonotonicMs_ = 0;
    qint64 lastWallMs_ = 0;
};
