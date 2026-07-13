import unittest

from store import tier_requirements_met


class TierThresholdBasisTest(unittest.TestCase):
    def test_total_recharge_uses_history_and_usage_gate(self) -> None:
        tier = {"threshold_amount": 100, "threshold_basis": "total_recharge", "usage_days": 3}

        self.assertTrue(tier_requirements_met(tier, 500, 0, {3: True}, {3: 0}))
        self.assertFalse(tier_requirements_met(tier, 500, 0, {3: False}, {3: 0}))

    def test_recent_recharge_uses_usage_window_amount(self) -> None:
        tier = {"threshold_amount": 100, "threshold_basis": "recent_recharge", "usage_days": 3}

        self.assertFalse(tier_requirements_met(tier, 500, 0, {3: True}, {3: 20}))
        self.assertTrue(tier_requirements_met(tier, 0, 0, {3: True}, {3: 120}))

    def test_current_balance_uses_balance_and_usage_gate(self) -> None:
        tier = {"threshold_amount": 100, "threshold_basis": "current_balance", "usage_days": 3}

        self.assertFalse(tier_requirements_met(tier, 500, 80, {3: True}, {3: 0}))
        self.assertTrue(tier_requirements_met(tier, 0, 120, {3: True}, {3: 0}))


if __name__ == "__main__":
    unittest.main()
