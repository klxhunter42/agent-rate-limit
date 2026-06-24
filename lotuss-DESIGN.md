# ENTERPRISE DESIGN SYSTEM SPECIFICATION
Version: 2.0

Purpose:
Unified Design System สำหรับ E-Commerce, Marketplace, Loyalty Program, Membership Platform, Mobile App, Dashboard, Backoffice, Admin Portal และ Gamification Platform

---

# DESIGN PRINCIPLES

1. Mobile First
2. Accessibility First
3. Performance First
4. Conversion First
5. Design Token First
6. Dark Mode Ready
7. Enterprise Scalable
8. Gamification Optional
9. Consistent Interaction
10. Responsive By Default

---

# BRAND PERSONALITY

Primary

Trustworthy
Modern
Professional
Friendly

Secondary

Rewarding
Fun
Engaging

Tertiary

Premium
Community Driven

---

# EMOTIONAL DESIGN

User Journey

Discover
Understand
Interact
Convert
Reward
Retain

Emotion Curve

Neutral
Interest
Excitement
Confidence
Achievement

---

# COLOR FOUNDATION

## BRAND PRIMARY

--brand-primary-50: #E8FFF2;
--brand-primary-100: #D1FFE5;
--brand-primary-200: #A3FFCB;
--brand-primary-300: #75FFB1;
--brand-primary-400: #47FF97;
--brand-primary-500: #00A651;
--brand-primary-600: #00964A;
--brand-primary-700: #007C3D;
--brand-primary-800: #005F2F;
--brand-primary-900: #003F1F;

Purpose

Primary CTA
Checkout
Revenue Actions
Navigation Active

---

## BRAND SECONDARY

--brand-secondary-50: #FFFCE5;
--brand-secondary-100: #FFF8CC;
--brand-secondary-200: #FFF099;
--brand-secondary-300: #FFE866;
--brand-secondary-400: #FFE133;
--brand-secondary-500: #FFDD00;
--brand-secondary-600: #E6C700;
--brand-secondary-700: #CCB000;
--brand-secondary-800: #998400;
--brand-secondary-900: #665800;

Purpose

Promotion
Campaign
Highlight
Rewards

Lotus's corporate identity is built around green, yellow and white palettes. ([Inside Retail Asia](https://insideretail.asia/2021/02/17/cp-group-rebrands-its-southeast-asia-tesco-stores/?utm_source=chatgpt.com))

---

## GAMIFICATION COLORS

--game-teal: #2BA8A2;
--game-coral: #EF6C4A;
--game-gold: #FFD23F;
--game-sky: #5DADE2;

Purpose

Achievement
Leaderboard
Ranking
Rewards
Events

---

## SEMANTIC COLORS

--success: #27AE60;
--warning: #F59E0B;
--error: #E74C3C;
--info: #2563EB;

---

## GRAYSCALE

--gray-50: #FAFAFA;
--gray-100: #F5F5F5;
--gray-200: #EEEEEE;
--gray-300: #E0E0E0;
--gray-400: #BDBDBD;
--gray-500: #9E9E9E;
--gray-600: #757575;
--gray-700: #616161;
--gray-800: #424242;
--gray-900: #212121;

---

# LIGHT THEME

--surface-page: #FFFFFF;
--surface-container: #F7F8FA;
--surface-card: #FFFFFF;
--surface-hover: #F3F4F6;

--text-primary: #212121;
--text-secondary: #616161;
--text-muted: #9E9E9E;

--border-primary: #E5E7EB;

---

# DARK THEME

--surface-page: #121212;
--surface-container: #1B1B1B;
--surface-card: #242424;
--surface-hover: #303030;

--text-primary: #FFFFFF;
--text-secondary: #D1D5DB;
--text-muted: #9CA3AF;

--border-primary: #404040;

---

# TYPOGRAPHY

Font Stack

Inter
Noto Sans Thai
Prompt
sans-serif

Weights

400
500
600
700
800

Scale

Display XXL = 96
Display XL = 72
Display LG = 64

H1 = 48
H2 = 40
H3 = 32
H4 = 24
H5 = 20
H6 = 18

Body LG = 18
Body MD = 16
Body SM = 14

Caption = 12

Overline = 11

Line Heights

Display = 110%
Heading = 120%
Body = 150%

---

# SPACING

4
8
12
16
24
32
40
48
64
80
96
128
160
192
256

---

# GRID

Mobile

4 Columns
16 Margin
16 Gutter

Tablet

8 Columns

Desktop

12 Columns

Container

1440px

Content Width

1280px

---

# BORDER RADIUS

4
8
12
16
24
32
999

Semantic

xs
sm
md
lg
xl
pill

---

# SHADOWS

shadow-xs

0 1px 2px rgba(0,0,0,.04)

shadow-sm

0 2px 4px rgba(0,0,0,.06)

shadow-md

0 4px 12px rgba(0,0,0,.08)

shadow-lg

0 8px 24px rgba(0,0,0,.12)

shadow-xl

0 16px 48px rgba(0,0,0,.16)

---

# GLOW SHADOWS

shadow-teal-glow

0 4px 20px rgba(43,168,162,.30)

shadow-coral-glow

0 4px 20px rgba(239,108,74,.35)

shadow-gold-glow

0 4px 20px rgba(255,210,63,.40)

shadow-sky-glow

0 4px 20px rgba(93,173,226,.30)

---

# MOTION

Duration

75ms
150ms
200ms
300ms
500ms
800ms

Easing

ease

ease-in

ease-out

ease-in-out

cubic-bezier(.34,1.56,.64,1)

Purpose

Hover
Focus
Expand
Collapse
Reward
Celebrate

---

# Z INDEX

dropdown = 1000
sticky = 1100
drawer = 1200
modal = 1300
toast = 1400
tooltip = 1500
celebration = 1600

---

# BUTTON SPEC

Primary

Background

brand-primary

Text

white

Height

44
48
56

Radius

8

States

Default
Hover
Pressed
Disabled
Loading

---

Secondary

Background

white

Border

gray-200

---

Ghost

Transparent

---

Danger

error

---

Reward

Gold Gradient

Glow

Bounce

---

# INPUT SPEC

Height

44
48
56

Radius

8

Border

gray-300

Focus

brand-primary

Focus Ring

0 0 0 3px rgba(0,166,81,.20)

States

Default
Hover
Focus
Disabled
Error
Success

---

# CARD SPEC

Commerce Card

Image

Title

Description

Price

Discount

CTA

---

Reward Card

Reward

Progress

Claim

---

Achievement Card

Badge

Title

Description

Progress

---

Leaderboard Card

Avatar

Rank

Score

Trend

---

# NAVIGATION

Desktop

Navbar

Mega Menu

Sidebar

Breadcrumb

Tabs

Pagination

Mobile

Bottom Navigation

Drawer Navigation

Floating Action Button

---

# TABLE SPEC

Density

Compact = 40

Default = 48

Comfortable = 56

States

Hover
Selected
Loading
Empty

---

# MODAL

Small = 480

Medium = 720

Large = 960

Fullscreen

---

# DRAWER

Mobile = 100vw

Desktop = 420

---

# TOAST

Success

Warning

Error

Info

Duration

3000ms

---

# ALERT

Success

Warning

Error

Info

---

# EMPTY STATE

Illustration

Headline

Description

Primary CTA

Secondary CTA

---

# LOADING

Skeleton

Shimmer

Progress Bar

Spinner

---

# BADGES

New

Hot

Popular

Promo

VIP

Winner

Silver

Bronze

Gold

BOOM

Flip7

---

# ANIMATIONS

Micro

Fade
Scale
Slide
Expand
Collapse

Reward

Confetti
Coin Drop
Glow Pulse
Badge Pop
Achievement Unlock

Attention

Pulse
Shake
Bounce
Wobble

---

# ACCESSIBILITY

WCAG AA

Minimum Contrast

4.5:1

Touch Target

44x44

Recommended

48x48

Gamification Action

72x72

Keyboard Navigation

Required

Screen Reader

Required

Reduced Motion Support

Required

---

# COMPONENT INVENTORY

Foundation

Button
IconButton
Input
Textarea
Select
Checkbox
Radio
Switch
Avatar
Badge
Tag
Tooltip

Navigation

Navbar
Sidebar
BottomNav
Breadcrumb
Tabs
MegaMenu
Pagination

Commerce

ProductCard
CategoryCard
CartDrawer
CheckoutStepper
CouponInput
OrderSummary

Gamification

Leaderboard
RewardCard
AchievementCard
WinnerCard
RankBadge
ProgressCard
ConfettiLayer

Data

Table
Timeline
StatsCard
ChartCard
DataGrid

Overlay

Modal
Drawer
Popover
Tooltip
Toast
BottomSheet

Feedback

Alert
EmptyState
LoadingState
Skeleton

---

# DESIGN TOKEN NAMING

color.brand.primary
color.brand.secondary

color.semantic.success
color.semantic.warning
color.semantic.error

surface.page
surface.container
surface.card

text.primary
text.secondary
text.muted

spacing.4
spacing.8
spacing.16

radius.sm
radius.md
radius.lg

shadow.sm
shadow.md
shadow.lg

motion.fast
motion.normal
motion.slow

z.dropdown
z.modal
z.toast

---

# DO

Use Design Tokens Everywhere

Use Semantic Colors

Use Responsive Layout

Use Accessibility Checks

Use Dark Mode Support

Use Consistent Motion

Use Loading States

Use Empty States

Use Error States

Use Skeleton Loading

---

# DONT

Do Not Hardcode Colors

Do Not Hardcode Spacing

Do Not Mix Multiple Accent Colors

Do Not Use More Than 2 Glow Effects Per Screen

Do Not Animate Longer Than 500ms For Micro Interactions

Do Not Use Pure Black

Do Not Use Pure White In Dark Theme

Do Not Use More Than 3 Heading Sizes On Same Screen

Do Not Create New Component Variants Without Design Review