package memory

import "testing"

// 009 FR-04: continuity detector.

func TestIsContinuityQuery_English(t *testing.T) {
	cases := []string{
		"What did we discuss on Monday?",
		"do you remember the IOH plan?",
		"last week you said we'd ship Friday",
		"as discussed earlier, please continue",
		"follow up on yesterday's deploy",
		"recap what we decided previously",
	}
	for _, c := range cases {
		if !isContinuityQuery(c) {
			t.Errorf("expected continuity for %q", c)
		}
	}
}

func TestIsContinuityQuery_Vietnamese(t *testing.T) {
	cases := []string{
		"bạn có nhớ kế hoạch IOH không?",
		"hôm qua chúng ta đã thảo luận gì?",
		"tuần trước bạn đã đề cập việc deploy",
		"nhắc lại lần trước nhé",
	}
	for _, c := range cases {
		if !isContinuityQuery(c) {
			t.Errorf("expected continuity (vi) for %q", c)
		}
	}
}

func TestIsContinuityQuery_Chinese(t *testing.T) {
	cases := []string{
		"你还记得星期一讨论的内容吗?",
		"上次你说要上线,现在怎么样了",
		"我们之前讨论过这个方案",
		"昨天的部署继续跟进一下",
	}
	for _, c := range cases {
		if !isContinuityQuery(c) {
			t.Errorf("expected continuity (zh) for %q", c)
		}
	}
}

func TestIsContinuityQuery_NegativeCases(t *testing.T) {
	cases := []string{
		"",
		"hello",
		"write a haiku about the sea",
		"what is 2 plus 2",
		"deploy the service now",      // no continuity term
		"generate a test plan",        // no continuity term
	}
	for _, c := range cases {
		if isContinuityQuery(c) {
			t.Errorf("expected NOT continuity for %q", c)
		}
	}
}
