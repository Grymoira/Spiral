#include "clockcontinuity.h"

#include <iostream>

namespace {
bool check(bool condition, const char *description) {
    if (!condition)
        std::cerr << "clock-continuity: " << description << '\n';
    return condition;
}
}

int main() {
    constexpr qint64 wall = 1700000000000LL;
    ClockContinuityGuard guard;
    if (!check(guard.observe(0, wall), "initial sample must initialize") ||
        !check(guard.observe(500, wall + 500), "normal progress rejected") ||
        !check(guard.observe(1500, wall + 1500), "matching clock progress rejected"))
        return 1;

    // On platforms where a monotonic clock excludes suspend, the wall clock
    // advances much further. The first callback after wake must detect it.
    if (!check(!guard.observe(1510, wall + 14510), "suspend gap was missed") ||
        !check(guard.observe(1610, wall + 14610), "fresh samples must recover"))
        return 1;

    // Backward clock jumps and monotonic timer resets are also unsafe.
    if (!check(!guard.observe(1710, wall + 14500), "backward wall clock accepted") ||
        !check(!guard.observe(100, wall + 14700), "reset monotonic timer accepted"))
        return 1;

    // Small wall-clock adjustments must not repeatedly freeze playback.
    if (!check(guard.observe(600, wall + 15000), "small drift wrongly rejected") ||
        !check(guard.observe(1600, wall + 16600), "drift at threshold wrongly rejected") ||
        !check(!guard.observe(2600, wall + 19600), "large forward wall adjustment accepted"))
        return 1;

    ClockContinuityGuard another;
    if (!check(another.observe(0, wall), "fresh second instance rejected") ||
        !check(another.observe(20000, wall + 20000),
               "matching long elapsed intervals rejected by clock guard"))
        return 1;
    std::cout << "clock continuity guard regressions passed\n";
    return 0;
}
