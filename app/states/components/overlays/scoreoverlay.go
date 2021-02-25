package overlays

import (
	"fmt"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/wieku/danser-go/app/audio"
	"github.com/wieku/danser-go/app/beatmap"
	"github.com/wieku/danser-go/app/beatmap/difficulty"
	"github.com/wieku/danser-go/app/beatmap/objects"
	"github.com/wieku/danser-go/app/bmath"
	camera2 "github.com/wieku/danser-go/app/bmath/camera"
	"github.com/wieku/danser-go/app/discord"
	"github.com/wieku/danser-go/app/graphics"
	"github.com/wieku/danser-go/app/input"
	"github.com/wieku/danser-go/app/rulesets/osu"
	"github.com/wieku/danser-go/app/settings"
	"github.com/wieku/danser-go/app/skin"
	"github.com/wieku/danser-go/app/states/components/common"
	"github.com/wieku/danser-go/app/states/components/overlays/play"
	"github.com/wieku/danser-go/app/storyboard"
	"github.com/wieku/danser-go/framework/bass"
	"github.com/wieku/danser-go/framework/graphics/batch"
	"github.com/wieku/danser-go/framework/graphics/font"
	"github.com/wieku/danser-go/framework/graphics/shape"
	"github.com/wieku/danser-go/framework/graphics/sprite"
	"github.com/wieku/danser-go/framework/math/animation"
	"github.com/wieku/danser-go/framework/math/animation/easing"
	color2 "github.com/wieku/danser-go/framework/math/color"
	"github.com/wieku/danser-go/framework/math/vector"
	"math"
	"strconv"
	"strings"
)

const (
	vAccOffset   = 4.8
	barWidth     = 272.0
	barThickness = 4.8
)

type Overlay interface {
	Update(float64)
	DrawBeforeObjects(batch *batch.QuadBatch, colors []color2.Color, alpha float64)
	DrawNormal(batch *batch.QuadBatch, colors []color2.Color, alpha float64)
	DrawHUD(batch *batch.QuadBatch, colors []color2.Color, alpha float64)
	IsBroken(cursor *graphics.Cursor) bool
	NormalBeforeCursor() bool
}

type ScoreOverlay struct {
	font     *font.Font
	lastTime float64
	combo    int64
	newCombo int64

	comboSlide     *animation.Glider
	newComboScale  *animation.Glider
	newComboScaleB *animation.Glider
	newComboFadeB  *animation.Glider

	currentScore int64
	displayScore float64

	currentAccuracy float64
	displayAccuracy float64

	ppGlider   *animation.Glider
	ruleset    *osu.OsuRuleSet
	cursor     *graphics.Cursor
	combobreak *bass.Sample
	music      *bass.Track
	nextEnd    float64
	results    *play.HitResults

	keyStates   [4]bool
	keyCounters [4]int
	lastPresses [4]float64
	keyOverlay  *sprite.SpriteManager
	keys        []*sprite.Sprite

	ScaledWidth  float64
	ScaledHeight float64
	camera       *camera2.Camera
	scoreFont    *font.Font
	comboFont    *font.Font
	scoreEFont   *font.Font

	bgDim *animation.Glider

	hitErrorMeter *play.HitErrorMeter

	skip *sprite.Sprite

	healthBackground *sprite.Sprite
	healthBar        *sprite.Sprite
	displayHp        float64

	hpSlide *animation.Glider
	hpFade  *animation.Glider

	shapeRenderer *shape.Renderer

	boundaries     *common.Boundaries
	hpBasePosition vector.Vector2d

	mods       *sprite.SpriteManager
	notFirst   bool
	flashlight *common.Flashlight
	delta      float64

	entry         *play.ScoreBoard
	audioTime     float64
	normalTime    float64
	breakMode     bool
	currentBreak  *beatmap.Pause
	sPass         *sprite.Sprite
	sFail         *sprite.Sprite
	passContainer *sprite.SpriteManager
}

func NewScoreOverlay(ruleset *osu.OsuRuleSet, cursor *graphics.Cursor) *ScoreOverlay {
	overlay := new(ScoreOverlay)
	overlay.ScaledHeight = 768
	overlay.ScaledWidth = settings.Graphics.GetAspectRatio() * overlay.ScaledHeight

	overlay.results = play.NewHitResults(ruleset.GetBeatMap().Diff)
	overlay.ruleset = ruleset
	overlay.cursor = cursor
	overlay.font = font.GetFont("Exo 2 Bold")

	overlay.comboSlide = animation.NewGlider(0)
	overlay.comboSlide.SetEasing(easing.OutQuad)

	overlay.newComboScale = animation.NewGlider(1.28)
	overlay.newComboScaleB = animation.NewGlider(1.28)
	overlay.newComboFadeB = animation.NewGlider(0)

	overlay.ppGlider = animation.NewGlider(0)
	overlay.ppGlider.SetEasing(easing.OutQuint)

	overlay.bgDim = animation.NewGlider(1)

	overlay.hpSlide = animation.NewGlider(0)
	overlay.hpFade = animation.NewGlider(1)

	overlay.combobreak = audio.LoadSample("combobreak")

	audio.LoadSample("sectionpass")
	audio.LoadSample("sectionfail")

	overlay.sPass = sprite.NewSpriteSingle(skin.GetTexture("section-pass"), 0, vector.NewVec2d(overlay.ScaledWidth, overlay.ScaledHeight).Scl(0.5), bmath.Origin.Centre)
	overlay.sPass.SetAlpha(0)

	overlay.sFail = sprite.NewSpriteSingle(skin.GetTexture("section-fail"), 0, vector.NewVec2d(overlay.ScaledWidth, overlay.ScaledHeight).Scl(0.5), bmath.Origin.Centre)
	overlay.sFail.SetAlpha(0)

	overlay.passContainer = sprite.NewSpriteManager()
	overlay.passContainer.Add(overlay.sPass)
	overlay.passContainer.Add(overlay.sFail)

	discord.UpdatePlay(cursor.Name)

	overlay.scoreEFont = skin.GetFont("scoreentry")
	overlay.scoreFont = skin.GetFont("score")
	overlay.comboFont = skin.GetFont("combo")

	ruleset.SetListener(func(cursor *graphics.Cursor, time int64, number int64, position vector.Vector2d, result osu.HitResult, comboResult osu.ComboResult, pp float64, score1 int64) {
		if result&(osu.BaseHitsM) > 0 {
			overlay.results.AddResult(time, result, position)
		}

		_, hC := ruleset.GetBeatMap().HitObjects[number].(*objects.Circle)
		allowCircle := hC && (result&osu.BaseHits > 0)
		_, sl := ruleset.GetBeatMap().HitObjects[number].(*objects.Slider)
		allowSlider := sl && result == osu.SliderStart

		if allowCircle || allowSlider {
			timeDiff := float64(time) - ruleset.GetBeatMap().HitObjects[number].GetStartTime()

			overlay.hitErrorMeter.Add(float64(time), timeDiff)
		}

		if comboResult == osu.ComboResults.Increase {

			overlay.newComboScaleB.Reset()
			overlay.newComboScaleB.AddEventS(overlay.normalTime, overlay.normalTime+300, 2, 1.28)

			overlay.newComboFadeB.Reset()
			overlay.newComboFadeB.AddEventS(overlay.normalTime, overlay.normalTime+300, 0.6, 0.0)

			overlay.animate(overlay.normalTime)

			overlay.combo = overlay.newCombo
			overlay.newCombo++
			overlay.nextEnd = overlay.normalTime + 300
		} else if comboResult == osu.ComboResults.Reset {
			if overlay.newCombo > 20 && overlay.combobreak != nil {
				overlay.combobreak.Play()
			}
			overlay.newCombo = 0
		}

		if overlay.flashlight != nil {
			overlay.flashlight.UpdateCombo(overlay.newCombo)
		}

		accuracy, mCombo, score, _ := overlay.ruleset.GetResults(overlay.cursor)

		overlay.entry.UpdatePlayer(score, mCombo)

		overlay.ppGlider.Reset()
		overlay.ppGlider.AddEvent(overlay.normalTime, overlay.normalTime+500, pp)

		overlay.currentScore = score
		overlay.currentAccuracy = accuracy
	})

	overlay.camera = camera2.NewCamera()
	overlay.camera.SetViewportF(0, int(overlay.ScaledHeight), int(overlay.ScaledWidth), 0)
	overlay.camera.Update()

	overlay.keyOverlay = sprite.NewSpriteManager()

	keyBg := sprite.NewSpriteSingle(skin.GetTexture("inputoverlay-background"), 0, vector.NewVec2d(overlay.ScaledWidth, overlay.ScaledHeight/2-64), bmath.Origin.TopLeft)
	keyBg.SetScaleV(vector.NewVec2d(1.05, 1))
	keyBg.ShowForever(true)
	keyBg.SetRotation(math.Pi / 2)

	overlay.keyOverlay.Add(keyBg)

	for i := 0; i < 4; i++ {
		posY := overlay.ScaledHeight/2 - 64 + (30.4+float64(i)*47.2)*settings.Gameplay.KeyOverlay.Scale

		key := sprite.NewSpriteSingle(skin.GetTexture("inputoverlay-key"), 1, vector.NewVec2d(overlay.ScaledWidth-24*settings.Gameplay.KeyOverlay.Scale, posY), bmath.Origin.Centre)
		key.ShowForever(true)

		overlay.keys = append(overlay.keys, key)
		overlay.keyOverlay.Add(key)
	}

	overlay.hitErrorMeter = play.NewHitErrorMeter(overlay.ScaledWidth, overlay.ScaledHeight, ruleset.GetBeatMap().Diff)

	start := overlay.ruleset.GetBeatMap().HitObjects[0].GetStartTime() - 2000

	if start > 2000 {
		skipFrames := skin.GetFrames("play-skip", true)
		overlay.skip = sprite.NewAnimation(skipFrames, skin.GetInfo().GetFrameTime(len(skipFrames)), true, 0.0, vector.NewVec2d(overlay.ScaledWidth, overlay.ScaledHeight), bmath.Origin.BottomRight)
		overlay.skip.SetAlpha(0.0)
		overlay.skip.AddTransform(animation.NewSingleTransform(animation.Fade, easing.OutQuad, 0, 500, 0.0, 0.6))
		overlay.skip.AddTransform(animation.NewSingleTransform(animation.Fade, easing.OutQuad, start, start+300, 0.6, 0.0))
	}

	overlay.healthBackground = sprite.NewSpriteSingle(skin.GetTexture("scorebar-bg"), 0, vector.NewVec2d(0, 0), bmath.Origin.TopLeft)

	pos := vector.NewVec2d(4.8, 16)
	if skin.GetTexture("scorebar-marker") != nil {
		pos = vector.NewVec2d(12, 12.5)
	}

	overlay.hpBasePosition = pos

	barTextures := skin.GetFrames("scorebar-colour", true)

	overlay.healthBar = sprite.NewAnimation(barTextures, skin.GetInfo().GetFrameTime(len(barTextures)), true, 0.0, pos, bmath.Origin.TopLeft)
	overlay.healthBar.SetCutOrigin(bmath.Origin.CentreLeft)

	overlay.shapeRenderer = shape.NewRenderer()

	overlay.boundaries = common.NewBoundaries()

	overlay.mods = sprite.NewSpriteManager()

	if overlay.ruleset.GetBeatMap().Diff.Mods.Active(difficulty.Flashlight) {
		overlay.flashlight = common.NewFlashlight(overlay.ruleset.GetBeatMap())
	}

	overlay.entry = play.NewScoreboard(overlay.ruleset.GetBeatMap(), overlay.cursor.ScoreID)
	overlay.entry.AddPlayer(overlay.cursor.Name)

	return overlay
}

func (overlay *ScoreOverlay) animate(time float64) {
	overlay.newComboScale.Reset()
	overlay.newComboScale.AddEventSEase(time, time+50, 1.28, 1.4, easing.InQuad)
	overlay.newComboScale.AddEventSEase(time+50, time+100, 1.4, 1.28, easing.OutQuad)
}

func (overlay *ScoreOverlay) Update(time float64) {
	if overlay.audioTime == 0 {
		overlay.audioTime = time
		overlay.normalTime = time
	}

	delta := time - overlay.audioTime

	if overlay.music != nil && overlay.music.GetState() == bass.MUSIC_PLAYING {
		delta /= settings.SPEED
	}

	overlay.normalTime += delta

	overlay.audioTime = time

	if !overlay.notFirst && time > -settings.Playfield.LeadInHold*1000 {
		overlay.notFirst = true

		mods := overlay.ruleset.GetBeatMap().Diff.Mods.StringFull()

		offset := -48.0
		for i, s := range mods {
			modSpriteName := "selection-mod-" + strings.ToLower(s)

			mod := sprite.NewSpriteSingle(skin.GetTexture(modSpriteName), float64(i), vector.NewVec2d(overlay.ScaledWidth+offset, 150), bmath.Origin.Centre)
			mod.SetAlpha(0)
			mod.ShowForever(true)

			timeStart := time + float64(i)*500

			mod.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, timeStart, timeStart+400, 0.0, 1.0))
			mod.AddTransform(animation.NewSingleTransform(animation.Scale, easing.OutQuad, timeStart, timeStart+400, 2, 1.0))

			if overlay.cursor.Name == "" {
				startT := overlay.ruleset.GetBeatMap().HitObjects[0].GetStartTime()
				mod.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, startT, timeStart+5000, 1.0, 0))

				endT := overlay.ruleset.GetBeatMap().HitObjects[len(overlay.ruleset.GetBeatMap().HitObjects)-1].GetEndTime()
				mod.AddTransform(animation.NewSingleTransform(animation.Fade, easing.OutQuad, endT, endT+500, 0.0, 1.0))

				offset -= 16
			} else {
				offset -= 80
			}

			overlay.mods.Add(mod)
		}
	}

	if input.Win.GetKey(glfw.KeySpace) == glfw.Press {
		if overlay.music != nil && overlay.music.GetState() == bass.MUSIC_PLAYING {
			start := overlay.ruleset.GetBeatMap().HitObjects[0].GetStartTime()
			if start-time > 4000 {
				overlay.music.SetPosition((start - 2000) / 1000)
			}
		}
	}

	overlay.results.Update(time)
	overlay.hitErrorMeter.Update(time)

	if overlay.skip != nil {
		overlay.skip.Update(time)
	}

	overlay.passContainer.Update(overlay.audioTime)

	//normal timing
	overlay.updateNormal(overlay.normalTime)
}

func (overlay *ScoreOverlay) updateNormal(time float64) {
	overlay.updateBreaks(time)


	if overlay.flashlight != nil && time >= 0 {

		overlay.flashlight.Update(time)
		overlay.flashlight.UpdatePosition(overlay.cursor.Position)

		proc := overlay.ruleset.GetProcessed()

		sliding := false
		for _, p := range proc {
			if o, ok := p.(*osu.Slider); ok {
				sliding = sliding || o.IsSliding(overlay.ruleset.GetPlayer(overlay.cursor))
			}
		}

		overlay.flashlight.SetSliding(sliding)
	}

	overlay.mods.Update(time)

	overlay.newComboScale.Update(time)
	overlay.newComboScaleB.Update(time)
	overlay.newComboFadeB.Update(time)
	overlay.ppGlider.Update(time)
	overlay.comboSlide.Update(time)
	overlay.hpFade.Update(time)
	overlay.hpSlide.Update(time)

	overlay.entry.Update(time)

	overlay.delta += time - overlay.lastTime
	if overlay.delta >= 16.6667 {
		overlay.delta -= 16.6667
		if overlay.combo > overlay.newCombo && overlay.newCombo == 0 {
			overlay.combo--
		}
	}

	if overlay.combo != overlay.newCombo && overlay.nextEnd < time+140 {
		overlay.animate(time)
		overlay.combo = overlay.newCombo
		overlay.nextEnd = math.MaxInt64
	}

	currentHp := overlay.ruleset.GetHP(overlay.cursor)

	delta60 := (time - overlay.lastTime) / 16.667

	if overlay.displayHp < currentHp {
		overlay.displayHp = math.Min(1.0, overlay.displayHp+math.Abs(currentHp-overlay.displayHp)/4*delta60)
	} else if overlay.displayHp > currentHp {
		overlay.displayHp = math.Max(0.0, overlay.displayHp-math.Abs(overlay.displayHp-currentHp)/6*delta60)
	}

	overlay.healthBar.SetCutX(1.0 - overlay.displayHp)

	if math.Abs(overlay.displayScore-float64(overlay.currentScore)) < 0.5 {
		overlay.displayScore = float64(overlay.currentScore)
	} else {
		overlay.displayScore = float64(overlay.currentScore) + (overlay.displayScore-float64(overlay.currentScore))*math.Pow(0.75, delta60)
	}

	if math.Abs(overlay.displayAccuracy-overlay.currentAccuracy) < 0.005 {
		overlay.displayAccuracy = overlay.currentAccuracy
	} else {
		overlay.displayAccuracy = overlay.currentAccuracy + (overlay.displayAccuracy-overlay.currentAccuracy)*math.Pow(0.5, delta60)
	}

	currentStates := [4]bool{overlay.cursor.LeftKey, overlay.cursor.RightKey, overlay.cursor.LeftMouse && !overlay.cursor.LeftKey, overlay.cursor.RightMouse && !overlay.cursor.RightKey}

	for i, state := range currentStates {
		color := color2.Color{R: 1.0, G: 222.0 / 255, B: 0, A: 0}
		if i > 1 {
			color = color2.Color{R: 248.0 / 255, G: 0, B: 158.0 / 255, A: 0}
		}

		if !overlay.keyStates[i] && state {
			key := overlay.keys[i]

			key.ClearTransformationsOfType(animation.Scale)
			key.AddTransform(animation.NewSingleTransform(animation.Scale, easing.OutQuad, time, time+100, 1.0, 0.8))
			key.AddTransform(animation.NewColorTransform(animation.Color3, easing.OutQuad, time, time+100, color2.Color{R: 1, G: 1, B: 1, A: 1}, color))

			overlay.lastPresses[i] = time + 100

			if overlay.isDrain() {
				overlay.keyCounters[i]++
			}
		}

		if overlay.keyStates[i] && !state {
			key := overlay.keys[i]
			key.ClearTransformationsOfType(animation.Scale)
			key.AddTransform(animation.NewSingleTransform(animation.Scale, easing.OutQuad, math.Max(time, overlay.lastPresses[i]), time+100, key.GetScale().Y, 1.0))

			key.AddTransform(animation.NewColorTransform(animation.Color3, easing.OutQuad, time, time+100, color, color2.Color{R: 1, G: 1, B: 1, A: 1}))
		}

		overlay.keyStates[i] = state
	}

	overlay.keyOverlay.Update(time)
	overlay.bgDim.Update(time)
	overlay.healthBackground.Update(time)
	overlay.healthBar.Update(time)

	overlay.lastTime = time
}

func (overlay *ScoreOverlay) updateBreaks(time float64) {
	inBreak := false

	for _, b := range overlay.ruleset.GetBeatMap().Pauses {
		if overlay.audioTime < b.GetStartTime() {
			break
		}

		if b.GetEndTime()-b.GetStartTime() >= 1000 && overlay.audioTime >= b.GetStartTime() && overlay.audioTime <= b.GetEndTime() {
			inBreak = true
			overlay.currentBreak = b
			break
		}
	}

	if !overlay.breakMode && inBreak {
		overlay.showPassInfo()

		overlay.comboSlide.AddEvent(time, time+500, -1)
		overlay.bgDim.AddEvent(time, time+500, 0)
		overlay.hpSlide.AddEvent(time, time+500, -20)
		overlay.hpFade.AddEvent(time, time+500, 0)
	} else if overlay.breakMode && !inBreak {
		overlay.comboSlide.AddEvent(time, time+500, 0)
		overlay.bgDim.AddEvent(time, time+500, 1)
		overlay.hpSlide.AddEvent(time, time+500, 0)
		overlay.hpFade.AddEvent(time, time+500, 1)
	}

	overlay.breakMode = inBreak
}

func (overlay *ScoreOverlay) SetMusic(music *bass.Track) {
	overlay.music = music
}

func (overlay *ScoreOverlay) DrawBeforeObjects(batch *batch.QuadBatch, _ []color2.Color, alpha float64) {
	overlay.boundaries.Draw(batch.Projection, float32(overlay.ruleset.GetBeatMap().Diff.CircleRadius), float32(alpha*overlay.bgDim.GetValue()))
}

func (overlay *ScoreOverlay) DrawNormal(batch *batch.QuadBatch, _ []color2.Color, alpha float64) {
	scale := overlay.ruleset.GetBeatMap().Diff.CircleRadius / 64
	batch.SetScale(scale, scale)

	overlay.results.Draw(batch, 1.0)

	batch.Flush()

	if overlay.flashlight != nil {
		overlay.flashlight.Draw(batch.Projection)
	}

	prev := batch.Projection
	batch.SetCamera(overlay.camera.GetProjectionView())

	overlay.hitErrorMeter.Draw(batch, alpha)

	batch.SetScale(1, 1)
	batch.SetColor(1, 1, 1, alpha)

	if overlay.skip != nil {
		overlay.skip.Draw(overlay.lastTime, batch)
	}

	batch.SetCamera(prev)
}

func (overlay *ScoreOverlay) DrawHUD(batch *batch.QuadBatch, _ []color2.Color, alpha float64) {
	prev := batch.Projection
	batch.SetCamera(overlay.camera.GetProjectionView())
	batch.ResetTransform()
	batch.SetColor(1, 1, 1, alpha)

	overlay.entry.Draw(batch, alpha)

	overlay.passContainer.Draw(overlay.audioTime, batch)

	overlay.drawScore(batch, alpha)
	overlay.drawCombo(batch, alpha)
	overlay.drawHP(batch, alpha)
	overlay.drawPP(batch, alpha)
	overlay.drawKeys(batch, alpha)

	batch.ResetTransform()
	batch.SetColor(1, 1, 1, alpha)

	overlay.mods.Draw(overlay.lastTime, batch)

	batch.SetCamera(prev)
}

func (overlay *ScoreOverlay) drawScore(batch *batch.QuadBatch, alpha float64) {
	scoreAlpha := settings.Gameplay.Score.Opacity * alpha

	if scoreAlpha < 0.001 || !settings.Gameplay.Score.Show {
		return
	}

	scoreScale := settings.Gameplay.Score.Scale
	rightOffset := -9.6 * scoreScale

	progress := overlay.getProgress()

	scoreSize := overlay.scoreFont.GetSize() * scoreScale * 0.96
	scoreOverlap := overlay.scoreFont.Overlap * scoreSize / overlay.scoreFont.GetSize()

	accSize := scoreSize * 0.6
	accOverlap := overlay.scoreFont.Overlap * accSize / overlay.scoreFont.GetSize()
	accYPos := scoreSize + vAccOffset*scoreScale

	accOffset := overlay.ScaledWidth - overlay.scoreFont.GetWidthMonospaced(accSize, "99.99%") + accOverlap - 38.4*scoreScale + rightOffset

	batch.Flush()

	overlay.shapeRenderer.SetCamera(overlay.camera.GetProjectionView())

	if settings.Gameplay.ProgressBar == "Pie" {
		if progress < 0.0 {
			overlay.shapeRenderer.SetColor(0.4, 0.8, 0.4, 0.6*scoreAlpha)
		} else {
			overlay.shapeRenderer.SetColor(1, 1, 1, 0.6*scoreAlpha)
		}

		overlay.shapeRenderer.Begin()
		overlay.shapeRenderer.DrawCircleProgressS(vector.NewVec2f(float32(accOffset), float32(accYPos+accSize/2)), 16*float32(settings.Gameplay.Score.Scale), 40, float32(progress))
		overlay.shapeRenderer.End()

		batch.SetColor(1, 1, 1, scoreAlpha)
		batch.SetScale(scoreScale, scoreScale)
		batch.SetTranslation(vector.NewVec2d(accOffset, accYPos+accSize/2))
		batch.DrawTexture(*skin.GetTextureSource("circularmetre", skin.LOCAL))

		accOffset -= 44.8 * scoreScale
	} else if progress > 0.0 {
		thickness := float32(barThickness * scoreScale)
		startPositionX := overlay.ScaledWidth - (12+barWidth)*scoreScale
		positionY := float32(scoreSize) - 2*float32(scoreScale) + thickness/2

		overlay.shapeRenderer.SetColor(1, 1, 0.5, 0.5*scoreAlpha)

		overlay.shapeRenderer.Begin()
		overlay.shapeRenderer.SetAdditive(true)
		overlay.shapeRenderer.DrawLine(float32(startPositionX), positionY, float32(startPositionX+progress*scoreScale*barWidth), positionY, thickness)
		overlay.shapeRenderer.SetAdditive(false)
		overlay.shapeRenderer.End()
	}

	batch.ResetTransform()
	batch.SetColor(1, 1, 1, scoreAlpha)

	scoreText := fmt.Sprintf("%08d", int64(math.Round(overlay.displayScore)))
	overlay.scoreFont.DrawOrigin(batch, overlay.ScaledWidth+rightOffset+scoreOverlap, 0, bmath.Origin.TopRight, scoreSize, true, scoreText)

	accText := fmt.Sprintf("%5.2f%%", overlay.displayAccuracy)
	overlay.scoreFont.DrawOrigin(batch, overlay.ScaledWidth+rightOffset+accOverlap, accYPos, bmath.Origin.TopRight, accSize, true, accText)

	if _, _, _, grade := overlay.ruleset.GetResults(overlay.cursor); grade != osu.NONE {
		gText := strings.ToLower(strings.ReplaceAll(osu.GradesText[grade], "SS", "X"))

		text := skin.GetTexture("ranking-" + gText + "-small")

		batch.SetTranslation(vector.NewVec2d(accOffset, accYPos+accSize/2))
		batch.SetSubScale(0.8*scoreScale, 0.8*scoreScale)
		batch.DrawTexture(*text)
	}
}

func (overlay *ScoreOverlay) drawCombo(batch *batch.QuadBatch, alpha float64) {
	comboAlpha := settings.Gameplay.ComboCounter.Opacity * alpha

	if comboAlpha < 0.001 || !settings.Gameplay.ComboCounter.Show {
		return
	}

	cmbSize := overlay.comboFont.GetSize() * settings.Gameplay.ComboCounter.Scale

	posX := overlay.comboSlide.GetValue()*overlay.comboFont.GetWidth(cmbSize*overlay.newComboScale.GetValue(), fmt.Sprintf("%dx", overlay.combo)) + 2.5
	posY := overlay.ScaledHeight - 12.8
	origY := overlay.comboFont.GetSize()*0.375 - 9

	batch.ResetTransform()

	batch.SetAdditive(true)

	batch.SetColor(1, 1, 1, overlay.newComboFadeB.GetValue()*comboAlpha)
	overlay.comboFont.DrawOrigin(batch, posX-2.4*overlay.newComboScaleB.GetValue()*settings.Gameplay.ComboCounter.Scale, posY+origY*overlay.newComboScaleB.GetValue()*settings.Gameplay.ComboCounter.Scale, bmath.Origin.BottomLeft, cmbSize*overlay.newComboScaleB.GetValue(), false, fmt.Sprintf("%dx", overlay.newCombo))

	batch.SetAdditive(false)

	batch.SetColor(1, 1, 1, comboAlpha)
	overlay.comboFont.DrawOrigin(batch, posX, posY+origY*overlay.newComboScale.GetValue()*settings.Gameplay.ComboCounter.Scale, bmath.Origin.BottomLeft, cmbSize*overlay.newComboScale.GetValue(), false, fmt.Sprintf("%dx", overlay.combo))
}

func (overlay *ScoreOverlay) drawHP(batch *batch.QuadBatch, alpha float64) {
	hpAlpha := settings.Gameplay.HpBar.Opacity * overlay.hpFade.GetValue() * alpha

	if hpAlpha < 0.001 || !settings.Gameplay.HpBar.Show {
		return
	}

	hpScale := settings.Gameplay.HpBar.Scale

	batch.ResetTransform()

	batch.SetScale(hpScale, hpScale)
	batch.SetTranslation(vector.NewVec2d(0, overlay.hpSlide.GetValue()))
	batch.SetColor(1, 1, 1, hpAlpha)

	overlay.healthBackground.Draw(overlay.lastTime, batch)

	overlay.healthBar.SetPosition(overlay.hpBasePosition.Scl(hpScale))
	overlay.healthBar.Draw(overlay.lastTime, batch)
}

func (overlay *ScoreOverlay) drawPP(batch *batch.QuadBatch, alpha float64) {
	ppAlpha := settings.Gameplay.PPCounter.Opacity * alpha

	if ppAlpha < 0.001 || !settings.Gameplay.PPCounter.Show {
		return
	}

	ppScale := settings.Gameplay.PPCounter.Scale

	batch.SetColor(1, 1, 1, ppAlpha)
	batch.SetScale(1, -1)
	batch.SetSubScale(1, 1)

	ppText := fmt.Sprintf("%.0fpp", overlay.ppGlider.GetValue())

	width := overlay.font.GetWidthMonospaced(40*ppScale, ppText)
	align := storyboard.Origin[settings.Gameplay.PPCounter.Align].AddS(1, -1).Mult(vector.NewVec2d(-width/2, -40*ppScale/2))

	overlay.font.DrawMonospaced(batch, settings.Gameplay.PPCounter.XPosition+align.X, settings.Gameplay.PPCounter.YPosition+align.Y, 40*ppScale, ppText)
}

func (overlay *ScoreOverlay) drawKeys(batch *batch.QuadBatch, alpha float64) {
	keyAlpha := settings.Gameplay.KeyOverlay.Opacity * alpha

	if keyAlpha < 0.001 || !settings.Gameplay.KeyOverlay.Show {
		return
	}

	batch.ResetTransform()

	keyScale := settings.Gameplay.KeyOverlay.Scale

	batch.SetColor(1, 1, 1, keyAlpha)
	batch.SetScale(keyScale, keyScale)

	overlay.keyOverlay.Draw(overlay.lastTime, batch)

	col := skin.GetInfo().InputOverlayText
	batch.SetColor(float64(col.R), float64(col.G), float64(col.B), keyAlpha)

	for i := 0; i < 4; i++ {
		posX := overlay.ScaledWidth - 24*keyScale
		posY := overlay.ScaledHeight/2 - 64 + (30.4+float64(i)*47.2)*keyScale
		scale := overlay.keys[i].GetScale().Y * keyScale

		text := strconv.Itoa(overlay.keyCounters[i])

		if overlay.keyCounters[i] == 0 || overlay.scoreEFont == nil {
			if overlay.keyCounters[i] == 0 {
				text = "K"
				if i > 1 {
					text = "M"
				}

				text += strconv.Itoa(i%2 + 1)
			}

			texLen := overlay.font.GetWidthMonospaced(scale*14, text)

			batch.SetScale(1, -1)
			overlay.font.DrawMonospaced(batch, posX-texLen/2, posY+scale*14/3, scale*14, text)
		} else {
			siz := scale * overlay.scoreEFont.GetSize()
			batch.SetScale(1, 1)
			overlay.scoreEFont.Overlap = 1.6
			overlay.scoreEFont.DrawOrigin(batch, posX, posY, bmath.Origin.Centre, siz, false, text)
		}
	}
}

func (overlay *ScoreOverlay) getProgress() float64 {
	hObjects := overlay.ruleset.GetBeatMap().HitObjects
	startTime := hObjects[0].GetStartTime()
	endTime := hObjects[len(hObjects)-1].GetEndTime()

	musicPos := 0.0
	if overlay.music != nil {
		musicPos = overlay.music.GetPosition() * 1000
	}

	progress := bmath.ClampF64((musicPos-startTime)/(endTime-startTime), 0.0, 1.0)
	if musicPos < startTime {
		progress = bmath.ClampF64(-1.0+musicPos/startTime, -1.0, 0.0)
	}

	return progress
}

func (overlay *ScoreOverlay) isDrain() bool {
	hObjects := overlay.ruleset.GetBeatMap().HitObjects
	startTime := hObjects[0].GetStartTime() - overlay.ruleset.GetBeatMap().Diff.Preempt
	endTime := hObjects[len(hObjects)-1].GetEndTime() + float64(overlay.ruleset.GetBeatMap().Diff.Hit50)

	return overlay.audioTime >= startTime && overlay.audioTime <= endTime && !overlay.breakMode
}

func (overlay *ScoreOverlay) IsBroken(_ *graphics.Cursor) bool {
	return false
}

func (overlay *ScoreOverlay) NormalBeforeCursor() bool {
	return true
}

func (overlay *ScoreOverlay) showPassInfo() {
	if overlay.currentBreak.Length() < 2880 {
		return
	}

	pass := overlay.ruleset.GetHP(overlay.cursor) >= 0.5

	time := math.Min(overlay.currentBreak.GetEndTime()-2880, overlay.currentBreak.GetEndTime()-overlay.currentBreak.Length()/2)

	if pass {
		overlay.passContainer.Add(audio.NewAudioSprite(audio.LoadSample("sectionpass"), time+20))

		overlay.sPass.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+20, time+20, 0, 1))
		overlay.sPass.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+100, time+100, 1, 0))
		overlay.sPass.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+160, time+160, 0, 1))
		overlay.sPass.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+230, time+230, 1, 0))
		overlay.sPass.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+280, time+280, 0, 1))
		overlay.sPass.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+1280, time+1480, 1, 0))
	} else {
		overlay.passContainer.Add(audio.NewAudioSprite(audio.LoadSample("sectionfail"), time+130))

		overlay.sFail.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+130, time+130, 0, 1))
		overlay.sFail.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+230, time+230, 1, 0))
		overlay.sFail.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+280, time+280, 0, 1))
		overlay.sFail.AddTransform(animation.NewSingleTransform(animation.Fade, easing.Linear, time+1280, time+1480, 1, 0))
	}
}
