package main

func (p *Plugin) runJob() {
	if currentHourInCET() != 6 {
		return
	}

	date := yesterdayInCET()

	var status RunStatus
	if err := p.kvstore.GetRunStatus(date, &status); err == nil {
		if status.Status == "completed" || status.Status == "completed_no_content" {
			p.API.LogInfo("Neil: morning processor already ran for this date, skipping", "date", date)
			return
		}
	}

	p.API.LogInfo("Neil: job triggering morning processor", "date", date)
	p.RunProcessor(date)
}
